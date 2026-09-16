package archive

import (
	"bufio"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

func buildFixity(root string, files []string) ([]FileInfo, error) {
	items := make([]FileInfo, 0, len(files))
	for _, relative := range files {
		clean, err := cleanRelative(relative)
		if err != nil {
			return nil, err
		}
		full := filepath.Join(root, filepath.FromSlash(clean))
		info, err := os.Stat(full)
		if err != nil {
			return nil, fmt.Errorf("固定性文件 %s：%w", clean, err)
		}
		if !info.Mode().IsRegular() {
			return nil, fmt.Errorf("固定性路径不是普通文件：%s", clean)
		}
		digest, err := hashFile(full)
		if err != nil {
			return nil, fmt.Errorf("计算 %s 固定性失败：%w", clean, err)
		}
		items = append(items, FileInfo{Path: clean, Size: info.Size(), SHA256: digest, Algorithm: "sha256"})
	}
	sortFiles(items)
	return items, nil
}

func writeFixity(filename string, files []FileInfo) error {
	file, err := os.OpenFile(filename, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o600)
	if err != nil {
		return err
	}
	for _, item := range files {
		if _, err := fmt.Fprintf(file, "%s  %s\n", item.SHA256, item.Path); err != nil {
			_ = file.Close()
			return err
		}
	}
	return file.Close()
}

// VerifyFixity 根据归档根目录中的普通文件校验 fixity/manifest.sha256。
func VerifyFixity(root string) ([]Issue, error) {
	_, issues, err := readFixity(root)
	return issues, err
}

func readFixity(root string) ([]FileInfo, []Issue, error) {
	filename := filepath.Join(root, "fixity", "manifest.sha256")
	file, err := os.Open(filename)
	if err != nil {
		return nil, nil, fmt.Errorf("打开固定性清单失败：%w", err)
	}
	defer file.Close()
	var files []FileInfo
	var issues []Issue
	scanner := bufio.NewScanner(file)
	seen := make(map[string]bool)
	for scanner.Scan() {
		line := scanner.Text()
		if len(line) < sha256.Size*2+2 || line[sha256.Size*2:sha256.Size*2+2] != "  " {
			issues = append(issues, Issue{Severity: "error", Code: "fixity.invalid_line", Message: "固定性清单存在无效行"})
			continue
		}
		digest := line[:sha256.Size*2]
		pathValue := line[sha256.Size*2+2:]
		if _, err := hex.DecodeString(digest); err != nil {
			issues = append(issues, Issue{Severity: "error", Code: "fixity.invalid_digest", Message: "固定性清单存在无效摘要：" + pathValue})
			continue
		}
		path, pathErr := cleanRelative(pathValue)
		if pathErr != nil {
			issues = append(issues, Issue{Severity: "error", Code: "fixity.invalid_path", Message: pathErr.Error()})
			continue
		}
		if seen[path] {
			issues = append(issues, Issue{Severity: "error", Code: "fixity.duplicate", Message: "固定性清单存在重复路径：" + path})
			continue
		}
		seen[path] = true
		files = append(files, FileInfo{Path: path, SHA256: strings.ToLower(digest), Algorithm: "sha256"})
	}
	if err := scanner.Err(); err != nil {
		return files, issues, err
	}
	for _, item := range files {
		fullPath := filepath.Join(root, filepath.FromSlash(item.Path))
		info, statErr := os.Lstat(fullPath)
		if statErr != nil {
			issues = append(issues, Issue{Severity: "error", Code: "fixity.missing", Message: fmt.Sprintf("固定性文件 %s 不可读取：%v", item.Path, statErr)})
			continue
		}
		if info.Mode()&os.ModeSymlink != 0 {
			issues = append(issues, Issue{Severity: "error", Code: "fixity.symlink", Message: "固定性路径不能是符号链接：" + item.Path})
			continue
		}
		actual, hashErr := hashFile(fullPath)
		if hashErr != nil {
			issues = append(issues, Issue{Severity: "error", Code: "fixity.missing", Message: fmt.Sprintf("固定性文件 %s 不可读取：%v", item.Path, hashErr)})
			continue
		}
		if !strings.EqualFold(item.SHA256, actual) {
			issues = append(issues, Issue{Severity: "error", Code: "fixity.mismatch", Message: "固定性摘要不匹配：" + item.Path})
		}
	}
	return files, issues, nil
}
