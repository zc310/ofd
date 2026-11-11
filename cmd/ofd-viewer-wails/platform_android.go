//go:build android

package main

/*
#include <stdlib.h>
char* ofdReaderCall(const char* method, const char* argument);
*/
import "C"

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
	"unsafe"

	"github.com/wailsapp/wails/v3/pkg/application"
)

const androidSaveTarget = "android-saf:document"

var (
	errAndroidStorage = errors.New("应用私有目录不可用（Android）")
	errAndroidBridge  = errors.New("原生文件接口不可用（Android）")
	errAndroidSave    = errors.New("系统保存失败（Android）")
	errAndroidTarget  = errors.New("保存目标无效或不可用（Android）")
)

//nolint:gochecknoinits // Wails 的 Android c-shared 入口必须在初始化时注册。
func init() {
	// c-shared 不会自动执行 main；Android 由 Java 宿主启动，不能调用 os.Exit。
	application.RegisterAndroidMain(func() {
		if err := prepareAndroidStorage(application.Android.StoragePath()); err != nil {
			fmt.Fprintln(os.Stderr, err)
			return
		}
		if err := run(""); err != nil {
			fmt.Fprintln(os.Stderr, err)
		}
	})
}

func prepareAndroidStorage(root string) error {
	if !filepath.IsAbs(root) {
		return errAndroidStorage
	}
	for key, directory := range map[string]string{
		"HOME": root, "XDG_CONFIG_HOME": filepath.Join(root, "config"),
		"XDG_CACHE_HOME": filepath.Join(root, "cache"), "TMPDIR": filepath.Join(root, "tmp"),
	} {
		if err := os.MkdirAll(directory, 0700); err != nil {
			return err
		}
		if err := os.Setenv(key, directory); err != nil {
			return err
		}
	}
	return nil
}

// 仅清理本应用创建的导入目录，保留当前文档、待解锁文档和五条最近记录。
// 调用者持有 openMu；不遍历或删除用户通过 SAF 选择的原始文件。
func (d *Desktop) prunePlatformImports() {
	root := filepath.Join(os.Getenv("HOME"), "imports")
	keep := map[string]bool{filepath.Dir(d.inputPath): true}
	for _, record := range d.recent {
		keep[filepath.Dir(record.Path)] = true
	}
	if d.unlock != nil {
		keep[filepath.Dir(d.unlock.path)] = true
	}
	directories, _ := os.ReadDir(root)
	for _, directory := range directories {
		path := filepath.Join(root, directory.Name())
		if !directory.IsDir() || keep[path] {
			continue
		}
		files, err := os.ReadDir(path)
		if err != nil {
			continue
		}
		for _, file := range files {
			if !file.IsDir() {
				_ = os.Remove(filepath.Join(path, file.Name()))
			}
		}
		_ = os.Remove(path)
	}
}

func releasePlatformCredential(path string) {
	parent := filepath.Dir(path)
	if filepath.Dir(parent) == filepath.Join(os.Getenv("HOME"), "imports") {
		_ = os.Remove(path)
		_ = os.Remove(parent)
	}
}

func androidCall(method, value string) (string, error) {
	name := C.CString(method)
	// JNI NewStringUTF 接受 Modified UTF-8；用 ASCII base64 安全传递任意文件名。
	argument := C.CString(base64.StdEncoding.EncodeToString([]byte(value)))
	defer C.free(unsafe.Pointer(name))
	defer C.free(unsafe.Pointer(argument))
	result := C.ofdReaderCall(name, argument)
	if result == nil {
		return "", errAndroidBridge
	}
	defer C.free(unsafe.Pointer(result))
	text := C.GoString(result)
	if text == "error" {
		return "", errAndroidSave
	}
	return text, nil
}

func (d *Desktop) savePicker(ctx context.Context, _ string) func(string) (string, error) {
	return func(name string) (string, error) {
		if err := ctx.Err(); err != nil {
			return "", err
		}
		if _, err := androidCall("saveStart", safeAttachmentFilename(name)); err != nil {
			return "", err
		}
		ticker := time.NewTicker(50 * time.Millisecond)
		defer ticker.Stop()
		for {
			status, err := androidCall("savePoll", "")
			if err != nil {
				releaseSaveTarget(androidSaveTarget)
				return "", err
			}
			switch status {
			case "ready":
				return androidSaveTarget, nil
			case "cancel":
				releaseSaveTarget(androidSaveTarget)
				return "", nil
			}
			select {
			case <-ctx.Done():
				releaseSaveTarget(androidSaveTarget)
				return "", ctx.Err()
			case <-ticker.C:
			}
		}
	}
}

func releaseSaveTarget(path string) {
	if path == androidSaveTarget {
		_, _ = androidCall("saveRelease", "")
	}
}

func (d *Desktop) checkSaveTarget(path string) error {
	if path == androidSaveTarget {
		return nil
	}
	return d.checkAttachmentTarget(path)
}

func writePlatformSave(ctx context.Context, path string, data []byte) (bool, error) {
	if path != androidSaveTarget {
		return false, nil
	}
	if err := ctx.Err(); err != nil {
		return true, err
	}
	// 直到权限复核和完整生成成功后才打开输出，不能在系统选择器返回时截断目标。
	value, err := androidCall("saveOpen", "")
	if err != nil {
		return true, err
	}
	fd, err := strconv.ParseUint(strings.TrimSpace(value), 10, 32)
	if err != nil {
		return true, errAndroidTarget
	}
	file := os.NewFile(uintptr(fd), "android-document")
	if file == nil {
		return true, errAndroidTarget
	}
	err = writeAndroidDocument(ctx, file, data)
	return true, errors.Join(err, file.Close())
}
