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
		wg.Add(1)
		go func(searchDir string) {
			defer wg.Done()

			// WalkDir 不跟随根目录本身的符号链接，先解析根目录。
			root, err := filepath.EvalSymlinks(searchDir)
			if err != nil {
				return
			}
			_ = filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
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
					// 跟随文件链接验证目标，排除断链、目录链接和 FIFO 等特殊文件。
					info, statErr := os.Stat(path)
					if statErr != nil || !info.Mode().IsRegular() {
						return nil
					}
					relative, relErr := filepath.Rel(root, path)
					if relErr != nil {
						return nil
					}

					if atomic.CompareAndSwapInt32(&found, 0, 1) {
						select {
						case resultChan <- filepath.Join(searchDir, relative):
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

// SamePath 先比较规范化的绝对路径，再按已存在文件的身份识别符号链接和硬链接。
// 路径相同但尚不存在时也返回 true；不同路径无法取得文件信息时返回 false。
func SamePath(left, right string) bool {
	leftAbs, leftErr := filepath.Abs(left)
	rightAbs, rightErr := filepath.Abs(right)
	if leftErr != nil || rightErr != nil {
		return false
	}
	if filepath.Clean(leftAbs) == filepath.Clean(rightAbs) {
		return true
	}
	leftInfo, leftErr := os.Stat(leftAbs)
	rightInfo, rightErr := os.Stat(rightAbs)
	return leftErr == nil && rightErr == nil && os.SameFile(leftInfo, rightInfo)
}
