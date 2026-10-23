package main

import (
	"archive/tar"
	"bytes"
	"crypto/sha256"
	"debug/elf"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"testing"
)

// TestThumbnailerNativePackages 在具备打包工具时检查真实安装包，不执行系统安装。
// 两个 Linux 发布任务均运行此测试，分别验证 amd64 和 arm64。
func TestThumbnailerNativePackages(t *testing.T) {
	if runtime.GOOS != "linux" || (runtime.GOARCH != "amd64" && runtime.GOARCH != "arm64") {
		t.Skip("原生安装包仅测试 Linux amd64/arm64")
	}
	nfpm := os.Getenv("NFPM")
	if nfpm == "" {
		nfpm = "nfpm"
	}
	nfpm, err := exec.LookPath(nfpm)
	if err != nil {
		t.Skip("原生安装包测试需要 nFPM；执行 make install-nfpm 或设置 NFPM")
	}
	root, err := filepath.Abs("../..")
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	build, dist := filepath.Join(dir, "build"), filepath.Join(dir, "dist")
	const version = "9.8.7"
	// 显式传入 CGO_ENABLED=1，验证系统包仍使用独立的静态构建。
	runPackageCommand(t, "make", "-C", root, "package-thumbnailer-linux", "GOOS=linux", "GOARCH="+runtime.GOARCH,
		"CGO_ENABLED=1", "VERSION="+version, "NFPM="+nfpm, "BUILD_DIR="+build, "DIST_DIR="+dist)
	binary := filepath.Join(build, "bin", "linux-"+runtime.GOARCH, "static", "ofd-thumbnailer")
	files := map[string]string{
		"usr/bin/zc310-ofd-thumbnailer":                            binary,
		"usr/share/thumbnailers/zc310-ofd.thumbnailer":             filepath.Join(root, "cmd/ofd-thumbnailer/zc310-ofd.thumbnailer"),
		"usr/share/doc/zc310-ofd-thumbnailer/README.md":            filepath.Join(root, "cmd/ofd-thumbnailer/README.md"),
		"usr/share/doc/zc310-ofd-thumbnailer/LICENSE":              filepath.Join(root, "LICENSE"),
		"usr/share/doc/zc310-ofd-thumbnailer/NOTICE":               filepath.Join(root, "NOTICE"),
		"usr/share/doc/zc310-ofd-thumbnailer/THIRD-PARTY-LICENSES": filepath.Join(root, "THIRD-PARTY-LICENSES"),
		"usr/share/doc/zc310-ofd-thumbnailer/ADOBE-GB1-NOTICE":     filepath.Join(root, "internal/cidmap/NOTICE"),
	}
	digests := map[string]string{}
	for name, source := range files {
		data, err := os.ReadFile(source)
		if err != nil {
			t.Fatal(err)
		}
		digests[name] = fmt.Sprintf("%x", sha256.Sum256(data))
	}
	program, err := elf.Open(binary)
	if err != nil {
		t.Fatal(err)
	}
	defer program.Close()
	wantMachine := elf.EM_X86_64
	rpmArch := "x86_64"
	if runtime.GOARCH == "arm64" {
		wantMachine, rpmArch = elf.EM_AARCH64, "aarch64"
	}
	if program.Machine != wantMachine {
		t.Fatalf("程序架构 = %v，期望 %v", program.Machine, wantMachine)
	}
	for _, header := range program.Progs {
		if header.Type == elf.PT_INTERP || header.Type == elf.PT_DYNAMIC {
			t.Fatal("系统安装包包含了动态链接程序")
		}
	}
	packagePath := func(t *testing.T, ext string) string {
		t.Helper()
		paths, err := filepath.Glob(filepath.Join(dist, "*."+ext))
		if err != nil || len(paths) != 1 {
			t.Fatalf("%s 产物数量错误: %v, %v", ext, paths, err)
		}
		return paths[0]
	}
	t.Run("deb", func(t *testing.T) {
		if _, err := exec.LookPath("dpkg-deb"); err != nil {
			t.Skip("DEB 内容检查需要 dpkg-deb")
		}
		path := packagePath(t, "deb")
		for field, want := range map[string]string{"Package": "zc310-ofd-thumbnailer", "Version": version + "-1", "Architecture": runtime.GOARCH} {
			if got := strings.TrimSpace(string(runPackageCommand(t, "dpkg-deb", "--field", path, field))); got != want {
				t.Fatalf("DEB %s = %q，期望 %q", field, got, want)
			}
		}
		archive := tar.NewReader(bytes.NewReader(runPackageCommand(t, "dpkg-deb", "--fsys-tarfile", path)))
		seen := map[string]bool{}
		for {
			header, err := archive.Next()
			if err == io.EOF {
				break
			}
			if err != nil {
				t.Fatal(err)
			}
			if header.Typeflag == tar.TypeDir {
				continue
			}
			name := strings.TrimPrefix(header.Name, "./")
			want, ok := digests[name]
			if !ok || seen[name] {
				t.Fatalf("DEB 中有非预期或重复的文件: %s", name)
			}
			data, err := io.ReadAll(archive)
			if err != nil || fmt.Sprintf("%x", sha256.Sum256(data)) != want {
				t.Fatalf("DEB 文件内容不一致: %s, %v", name, err)
			}
			mode := int64(0644)
			if name == "usr/bin/zc310-ofd-thumbnailer" {
				mode = 0755
			}
			if header.Mode != mode || header.Uid != 0 || header.Gid != 0 {
				t.Fatalf("DEB 文件权限错误: %s: %o %d:%d", name, header.Mode, header.Uid, header.Gid)
			}
			seen[name] = true
		}
		if len(seen) != len(files) {
			t.Fatalf("DEB 缺少文件: %v", seen)
		}
	})
	t.Run("rpm", func(t *testing.T) {
		if _, err := exec.LookPath("rpm"); err != nil {
			t.Skip("RPM 内容检查需要 rpm；Linux 发布任务会安装此工具")
		}
		path := packagePath(t, "rpm")
		metadata := runPackageCommand(t, "rpm", "-qp", "--queryformat", "%{NAME}\n%{VERSION}-%{RELEASE}\n%{ARCH}\n%{FILEDIGESTALGO}\n", path)
		want := "zc310-ofd-thumbnailer\n" + version + "-1\n" + rpmArch + "\n8\n"
		if string(metadata) != want {
			t.Fatalf("RPM 元数据错误: %s，期望 %s", metadata, want)
		}
		runPackageCommand(t, "rpm", "-K", "--nosignature", path)
		listing := runPackageCommand(t, "rpm", "-qp", "--queryformat", "[%{FILENAMES}\t%{FILEMODES:octal}\t%{FILEUSERNAME}\t%{FILEGROUPNAME}\t%{FILEDIGESTS}\n]", path)
		seen := map[string]bool{}
		for _, line := range strings.Split(strings.TrimSpace(string(listing)), "\n") {
			fields := strings.Split(line, "\t")
			if len(fields) != 5 {
				t.Fatalf("RPM 文件记录错误: %q", line)
			}
			mode, err := strconv.ParseUint(fields[1], 8, 32)
			if err != nil {
				t.Fatal(err)
			}
			if mode&0170000 == 0040000 {
				continue
			}
			name := strings.TrimPrefix(fields[0], "/")
			want, ok := digests[name]
			if !ok || seen[name] || fields[4] != want {
				t.Fatalf("RPM 文件内容不一致或存在额外文件: %q", line)
			}
			wantMode := uint64(0100644)
			if name == "usr/bin/zc310-ofd-thumbnailer" {
				wantMode = 0100755
			}
			if mode != wantMode || fields[2] != "root" || fields[3] != "root" {
				t.Fatalf("RPM 文件权限错误: %q", line)
			}
			seen[name] = true
		}
		if len(seen) != len(files) {
			t.Fatalf("RPM 缺少文件: %v", seen)
		}
	})
}

func runPackageCommand(t *testing.T, name string, args ...string) []byte {
	t.Helper()
	command := exec.CommandContext(t.Context(), name, args...)
	var diagnostics bytes.Buffer
	command.Stderr = &diagnostics
	output, err := command.Output()
	if err != nil {
		t.Fatalf("%s %v 失败: %v\n%s\n%s", name, args, err, output, diagnostics.Bytes())
	}
	return output
}
