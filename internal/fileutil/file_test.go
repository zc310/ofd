package fileutil

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func TestFindFirstFileInDirsSupportsMultipleTargetFiles(t *testing.T) {
	dir := t.TempDir()
	want := filepath.Join(dir, "nested", "SimHei.TTF")
	if err := os.MkdirAll(filepath.Dir(want), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(want, []byte("font"), 0o644); err != nil {
		t.Fatal(err)
	}

	got, err := FindFirstFileInDirs([]string{dir}, "simsun.ttc", "simhei.ttf")
	if err != nil {
		t.Fatal(err)
	}
	if got != want {
		t.Fatalf("FindFirstFileInDirs() = %q, want %q", got, want)
	}
}

func TestSamePathComparesCleanedAbsolutePaths(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "a", "b.ofd")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("ofd"), 0o644); err != nil {
		t.Fatal(err)
	}
	joined := filepath.Join(dir, "a", "x", "..", "b.ofd")
	if !SamePath(path, joined) {
		t.Errorf("SamePath(%q, %q) = false, want true", path, joined)
	}
	if SamePath(path, filepath.Join(dir, "other.ofd")) {
		t.Error("不同文件路径不应视为同一文件")
	}
}

func TestSamePathResolvesSymlinks(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "target.ofd")
	if err := os.WriteFile(target, []byte("ofd"), 0o644); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(dir, "link.ofd")
	if err := os.Symlink(target, link); err != nil {
		t.Skipf("无法创建符号链接: %v", err)
	}
	if !SamePath(target, link) {
		t.Errorf("SamePath(%q, %q) = false, 符号链接应视为同一文件", target, link)
	}
}

func TestSamePathRecognizesHardlinks(t *testing.T) {
	dir := t.TempDir()
	input := filepath.Join(dir, "input.ofd")
	output := filepath.Join(dir, "report.txt")
	if err := os.WriteFile(input, []byte("ofd"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Link(input, output); err != nil {
		t.Skipf("无法创建硬链接: %v", err)
	}
	if !SamePath(input, output) || !SamePath(output, input) {
		t.Fatal("输出是输入的硬链接时，应识别为同一文件")
	}
}

func TestSamePathRecognizesWindowsCaseAliases(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("测试 Windows 默认不区分大小写的路径")
	}
	dir := t.TempDir()
	path := filepath.Join(dir, "Input.ofd")
	alias := filepath.Join(dir, "input.ofd")
	if err := os.WriteFile(path, []byte("ofd"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(alias); os.IsNotExist(err) {
		t.Skip("测试目录区分大小写")
	} else if err != nil {
		t.Fatal(err)
	}
	if !SamePath(path, alias) {
		t.Fatal("大小写不同但指向同一文件的路径应相等")
	}
}

func TestSamePathComparesMissingPathsLexically(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "missing.ofd")
	alias := dir + string(filepath.Separator) + "." + string(filepath.Separator) + "missing.ofd"
	if !SamePath(path, alias) {
		t.Fatal("尚不存在但规范化后相同的路径应相等")
	}
	if SamePath(path, filepath.Join(dir, "other.ofd")) {
		t.Fatal("不同的缺失路径不应相等")
	}
}

func TestFindFirstFileInDirsSearchesSymlinkRoot(t *testing.T) {
	dir := t.TempDir()
	actual := filepath.Join(dir, "actual-fonts")
	if err := os.Mkdir(actual, 0700); err != nil {
		t.Fatal(err)
	}
	want := filepath.Join(actual, "font.ttf")
	if err := os.WriteFile(want, []byte("font"), 0600); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(dir, "linked-fonts")
	if err := os.Symlink(actual, link); err != nil {
		t.Skipf("无法创建目录符号链接: %v", err)
	}
	got, err := FindFirstFileInDirs([]string{link}, "font.ttf")
	if err != nil || !SamePath(got, want) {
		t.Fatalf("搜索链接目录 = %q, %v; want %q", got, err, want)
	}
}

func TestFindFirstFileInDirsValidatesSymlinkTargets(t *testing.T) {
	for _, targetType := range []string{"missing", "directory", "regular"} {
		t.Run(targetType, func(t *testing.T) {
			dir := t.TempDir()
			target := filepath.Join(t.TempDir(), "target")
			switch targetType {
			case "directory":
				if err := os.Mkdir(target, 0700); err != nil {
					t.Fatal(err)
				}
			case "regular":
				if err := os.WriteFile(target, []byte("font"), 0600); err != nil {
					t.Fatal(err)
				}
			}
			link := filepath.Join(dir, "font.ttf")
			if err := os.Symlink(target, link); err != nil {
				t.Skipf("无法创建符号链接: %v", err)
			}
			want := filepath.Join(dir, "nested", "font.ttf")
			if err := os.Mkdir(filepath.Dir(want), 0700); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(want, []byte("font"), 0600); err != nil {
				t.Fatal(err)
			}
			if targetType == "regular" {
				want = link
			}
			got, err := FindFirstFileInDirs([]string{dir}, "font.ttf")
			if err != nil || got != want {
				t.Fatalf("搜索 %s 链接 = %q, %v; want %q", targetType, got, err, want)
			}
		})
	}
}
