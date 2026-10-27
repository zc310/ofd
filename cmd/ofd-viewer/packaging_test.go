//go:build linux

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
	"slices"
	"strconv"
	"strings"
	"testing"
)

// TestViewerNativePackages 检查真实安装包，发布任务分别在两个架构上运行。
func TestViewerNativePackages(t *testing.T) {
	if runtime.GOARCH != "amd64" && runtime.GOARCH != "arm64" {
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
	// 显式关闭 CGO，检查系统包仍使用独立的 CGO 构建。
	runViewerPackageCommand(t, "make", "-C", root, "package-viewer-linux", "GOOS=linux", "GOARCH="+runtime.GOARCH,
		"CGO_ENABLED=0", "VERSION="+version, "NFPM="+nfpm, "BUILD_DIR="+build, "DIST_DIR="+dist)
	binary := filepath.Join(build, "bin", "linux-"+runtime.GOARCH, "native", "ofd-viewer")
	program, err := elf.Open(binary)
	if err != nil {
		t.Fatal(err)
	}
	defer program.Close()
	machine, rpmArch := elf.EM_X86_64, "x86_64"
	if runtime.GOARCH == "arm64" {
		machine, rpmArch = elf.EM_AARCH64, "aarch64"
	}
	if program.Machine != machine || program.Section(".dynamic") == nil {
		t.Fatalf("程序必须是目标架构的动态 ELF: %v", program.Machine)
	}
	files := map[string]string{
		"usr/bin/ofd-viewer": binary,
		"usr/share/applications/io.github.zc310.ofd.desktop":            filepath.Join(root, "cmd/ofd-viewer/share/io.github.zc310.ofd.desktop"),
		"usr/share/icons/hicolor/scalable/apps/io.github.zc310.ofd.svg": filepath.Join(root, "cmd/ofd-viewer/share/io.github.zc310.ofd.svg"),
		"usr/share/mime/packages/io.github.zc310.ofd.xml":               filepath.Join(root, "cmd/ofd-viewer/share/io.github.zc310.ofd.xml"),
		"usr/share/doc/zc310-ofd-viewer/README.md":                      filepath.Join(root, "cmd/ofd-viewer/README.md"),
		"usr/share/doc/zc310-ofd-viewer/LICENSE":                        filepath.Join(root, "LICENSE"),
		"usr/share/doc/zc310-ofd-viewer/NOTICE":                         filepath.Join(root, "NOTICE"),
		"usr/share/doc/zc310-ofd-viewer/THIRD-PARTY-LICENSES":           filepath.Join(root, "THIRD-PARTY-LICENSES"),
		"usr/share/doc/zc310-ofd-viewer/ADOBE-GB1-NOTICE":               filepath.Join(root, "internal/cidmap/NOTICE"),
	}
	digests := map[string]string{}
	for name, source := range files {
		data, err := os.ReadFile(source)
		if err != nil {
			t.Fatal(err)
		}
		digests[name] = fmt.Sprintf("%x", sha256.Sum256(data))
	}
	cacheScript, err := os.ReadFile("update-caches.sh")
	if err != nil {
		t.Fatal(err)
	}
	libraries, err := program.ImportedLibraries()
	if err != nil {
		t.Fatal(err)
	}
	symbols, err := program.ImportedSymbols()
	if err != nil {
		t.Fatal(err)
	}
	var maxVersion [3]int
	glibcVersion := ""
	for _, symbol := range symbols {
		v, ok := strings.CutPrefix(symbol.Version, "GLIBC_")
		if !ok {
			continue
		}
		var parts [3]int
		for i, part := range strings.Split(v, ".") {
			if i >= len(parts) {
				t.Fatalf("未知的 glibc 版本: %q", v)
			}
			parts[i], err = strconv.Atoi(part)
			if err != nil {
				t.Fatal(err)
			}
		}
		if slices.Compare(parts[:], maxVersion[:]) > 0 {
			maxVersion, glibcVersion = parts, v
		}
	}
	if glibcVersion == "" {
		t.Fatal("程序没有 glibc 版本信息")
	}
	glibcDependency := "libc6 (>= " + glibcVersion + ")"
	debLibraries := map[string]string{
		// 两种架构的动态加载器均由 libc6 提供，ARM64 还可能在 DT_NEEDED 中显式引用。
		"ld-linux-x86-64.so.2": glibcDependency, "ld-linux-aarch64.so.1": glibcDependency,
		"libc.so.6": glibcDependency, "libm.so.6": glibcDependency, "libpthread.so.0": glibcDependency, "libdl.so.2": glibcDependency, "librt.so.1": glibcDependency,
		"libGL.so.1": "libgl1", "libX11.so.6": "libx11-6", "libXcursor.so.1": "libxcursor1", "libXrandr.so.2": "libxrandr2",
		"libXinerama.so.1": "libxinerama1", "libXi.so.6": "libxi6", "libXxf86vm.so.1": "libxxf86vm1",
		"libwayland-client.so.0": "libwayland-client0", "libwayland-cursor.so.0": "libwayland-cursor0", "libwayland-egl.so.1": "libwayland-egl1",
		"libxkbcommon.so.0": "libxkbcommon0", "libEGL.so.1": "libegl1",
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
		for field, want := range map[string]string{"Package": "zc310-ofd-viewer", "Version": version + "-1", "Architecture": runtime.GOARCH} {
			if got := strings.TrimSpace(string(runViewerPackageCommand(t, "dpkg-deb", "--field", path, field))); got != want {
				t.Fatalf("DEB %s = %q，期望 %q", field, got, want)
			}
		}
		deps := strings.Split(strings.TrimSpace(string(runViewerPackageCommand(t, "dpkg-deb", "--field", path, "Depends"))), ", ")
		checkViewerPackageDependencies(t, deps, []string{"zenity", "xdg-utils", "shared-mime-info", "desktop-file-utils", "hicolor-icon-theme"})
		checkViewerPackageDependencies(t, deps, []string{glibcDependency, "libwayland-cursor0", "libwayland-egl1", "libxkbcommon0", "libegl1"})
		for _, lib := range libraries {
			dep, ok := debLibraries[lib]
			if !ok {
				t.Fatalf("动态库没有对应的 DEB 依赖: %s", lib)
			}
			checkViewerPackageDependencies(t, deps, []string{dep})
		}
		archive := tar.NewReader(bytes.NewReader(runViewerPackageCommand(t, "dpkg-deb", "--fsys-tarfile", path)))
		seen := map[string]bool{}
		for {
			h, err := archive.Next()
			if err == io.EOF {
				break
			}
			if err != nil {
				t.Fatal(err)
			}
			if h.Typeflag == tar.TypeDir {
				continue
			}
			name := strings.TrimPrefix(h.Name, "./")
			data, err := io.ReadAll(archive)
			mode := int64(0644)
			if name == "usr/bin/ofd-viewer" {
				mode = 0755
			}
			if err != nil || seen[name] || digests[name] != fmt.Sprintf("%x", sha256.Sum256(data)) || h.Mode != mode || h.Uid != 0 || h.Gid != 0 {
				t.Fatalf("DEB 文件内容或权限错误: %s, %v", name, err)
			}
			seen[name] = true
		}
		if len(seen) != len(files) {
			t.Fatalf("DEB 缺少文件: %v", seen)
		}
		control := filepath.Join(t.TempDir(), "control")
		runViewerPackageCommand(t, "dpkg-deb", "--control", path, control)
		for _, name := range []string{"postinst", "postrm"} {
			data, err := os.ReadFile(filepath.Join(control, name))
			if err != nil || !bytes.Equal(data, cacheScript) {
				t.Fatalf("DEB 缓存脚本错误: %s, %v", name, err)
			}
		}
	})
	t.Run("rpm", func(t *testing.T) {
		if _, err := exec.LookPath("rpm"); err != nil {
			t.Skip("RPM 内容检查需要 rpm；Linux 发布任务会安装此工具")
		}
		path := packagePath(t, "rpm")
		metadata := runViewerPackageCommand(t, "rpm", "-qp", "--queryformat", "%{NAME}\n%{VERSION}-%{RELEASE}\n%{ARCH}\n%{FILEDIGESTALGO}\n", path)
		if want := "zc310-ofd-viewer\n" + version + "-1\n" + rpmArch + "\n8\n"; string(metadata) != want {
			t.Fatalf("RPM 元数据错误: %s，期望 %s", metadata, want)
		}
		runViewerPackageCommand(t, "rpm", "-K", "--nosignature", path)
		deps := strings.Split(strings.TrimSpace(string(runViewerPackageCommand(t, "rpm", "-qp", "--requires", path))), "\n")
		loader := "ld-linux-x86-64.so.2"
		if runtime.GOARCH == "arm64" {
			loader = "ld-linux-aarch64.so.1"
		}
		checkViewerPackageDependencies(t, deps, []string{loader + "()(64bit)"})
		checkViewerPackageDependencies(t, deps, []string{"zenity", "xdg-utils", "shared-mime-info", "desktop-file-utils", "hicolor-icon-theme"})
		checkViewerPackageDependencies(t, deps, []string{"/bin/sh", "glibc >= " + glibcVersion, "libwayland-cursor.so.0()(64bit)", "libwayland-egl.so.1()(64bit)", "libxkbcommon.so.0()(64bit)", "libEGL.so.1()(64bit)"})
		for _, lib := range libraries {
			checkViewerPackageDependencies(t, deps, []string{lib + "()(64bit)"})
		}
		listing := runViewerPackageCommand(t, "rpm", "-qp", "--queryformat", "[%{FILENAMES}\t%{FILEMODES:octal}\t%{FILEUSERNAME}\t%{FILEGROUPNAME}\t%{FILEDIGESTS}\n]", path)
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
			wantMode := uint64(0100644)
			if name == "usr/bin/ofd-viewer" {
				wantMode = 0100755
			}
			if seen[name] || digests[name] != fields[4] || fields[4] == "" || mode != wantMode || fields[2] != "root" || fields[3] != "root" {
				t.Fatalf("RPM 文件内容或权限错误: %q", line)
			}
			seen[name] = true
		}
		if len(seen) != len(files) {
			t.Fatalf("RPM 缺少文件: %v", seen)
		}
		for _, tag := range []string{"POSTTRANS", "POSTUN"} {
			data := runViewerPackageCommand(t, "rpm", "-qp", "--queryformat", "%{"+tag+"}", path)
			if !bytes.Equal(bytes.TrimSpace(data), bytes.TrimSpace(cacheScript)) {
				t.Fatalf("RPM 缓存脚本错误: %s: %s", tag, data)
			}
		}
	})
}

// TestViewerPackageCacheUpdates 使用命令桩，覆盖安装、升级、卸载及缓存命令失败。
func TestViewerPackageCacheUpdates(t *testing.T) {
	dir := t.TempDir()
	logPath := filepath.Join(dir, "calls")
	for _, name := range []string{"update-desktop-database", "update-mime-database", "gtk-update-icon-cache"} {
		stub := "#!/bin/sh\nprintf '%s:%s\\n' \"${0##*/}\" \"$*\" >> \"$OFD_PACKAGE_CACHE_LOG\"\nexit 9\n"
		if err := os.WriteFile(filepath.Join(dir, name), []byte(stub), 0755); err != nil {
			t.Fatal(err)
		}
	}
	t.Setenv("PATH", dir)
	t.Setenv("OFD_PACKAGE_CACHE_LOG", logPath)
	for _, arg := range []string{"configure", "triggered", "remove", "purge", "0", "1", "2", "abort-upgrade", "upgrade", ""} {
		t.Run(arg, func(t *testing.T) {
			if err := os.WriteFile(logPath, nil, 0600); err != nil {
				t.Fatal(err)
			}
			runViewerPackageCommand(t, "/bin/sh", "update-caches.sh", arg)
			data, err := os.ReadFile(logPath)
			if err != nil {
				t.Fatal(err)
			}
			want := "update-desktop-database:/usr/share/applications\nupdate-mime-database:/usr/share/mime\ngtk-update-icon-cache:-q -t -f /usr/share/icons/hicolor\n"
			if arg == "abort-upgrade" || arg == "upgrade" || arg == "" {
				want = ""
			}
			if string(data) != want {
				t.Fatalf("缓存更新命令 = %q，期望 %q", data, want)
			}
		})
	}
}

func checkViewerPackageDependencies(t *testing.T, got, want []string) {
	t.Helper()
	for _, dep := range want {
		found := false
		for _, item := range got {
			if item == dep {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("缺少运行依赖 %q: %v", dep, got)
		}
	}
}

func runViewerPackageCommand(t *testing.T, name string, args ...string) []byte {
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
