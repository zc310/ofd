// Package utils 提供文件查找等内部通用工具。
package utils

import (
	"context"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

func FindFirstFileInDirs(dirs []string, targetFiles ...string) (string, error) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	targetNames := make(map[string]struct{}, len(targetFiles))
	for _, targetFile := range targetFiles {
		targetNames[strings.ToLower(targetFile)] = struct{}{}
	}

	var wg sync.WaitGroup
	resultChan := make(chan string, 1)
	found := int32(0)

	// 启动所有搜索goroutine
	for _, dir := range dirs {
		if _, err := os.Stat(dir); os.IsNotExist(err) {
			continue
		}

		wg.Add(1)
		go func(searchDir string) {
			defer wg.Done()

			_ = filepath.WalkDir(searchDir, func(path string, d fs.DirEntry, err error) error {
				// 检查是否已被取消
				select {
				case <-ctx.Done():
					return filepath.SkipAll
				default:
				}

				if atomic.LoadInt32(&found) == 1 {
					return filepath.SkipAll
				}

				if err != nil {
					return nil
				}

				if !d.IsDir() {
					if _, ok := targetNames[strings.ToLower(filepath.Base(path))]; !ok {
						return nil
					}

					if atomic.CompareAndSwapInt32(&found, 0, 1) {
						select {
						case resultChan <- path:
							cancel() // 取消其他goroutine
						default:
						}
					}
					return filepath.SkipAll
				}

				return nil
			})
		}(dir)
	}

	// 等待所有goroutine完成或找到文件
	go func() {
		wg.Wait()
		close(resultChan)
	}()

	// 尝试获取结果
	select {
	case result, ok := <-resultChan:
		if ok {
			return result, nil
		}
		return "", fmt.Errorf("未找到 %s", strings.Join(targetFiles, ", "))
	case <-time.After(30 * time.Second): // 添加超时防止永久阻塞
		cancel()
		return "", fmt.Errorf("搜索超时")
	}
}

// SamePath 判断两个文件路径是否指向同一文件。先按绝对路径与路径清理结果比较，
// 不相等时再尝试解析符号链接；任一环节出错返回 false。
func SamePath(left, right string) bool {
	leftAbs, leftErr := filepath.Abs(left)
	rightAbs, rightErr := filepath.Abs(right)
	if leftErr != nil || rightErr != nil {
		return false
	}
	if filepath.Clean(leftAbs) == filepath.Clean(rightAbs) {
		return true
	}
	leftReal, leftErr := filepath.EvalSymlinks(leftAbs)
	rightReal, rightErr := filepath.EvalSymlinks(rightAbs)
	return leftErr == nil && rightErr == nil && filepath.Clean(leftReal) == filepath.Clean(rightReal)
}
