package utils

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
)

func FindFirstFileInDirs(dirs []string, targetFile string) (string, error) {
	var found int32

	resultChan := make(chan string, 1)
	done := make(chan struct{})

	for _, dir := range dirs {
		if _, err := os.Stat(dir); os.IsNotExist(err) {
			continue
		}

		// 为每个目录启动一个goroutine
		go func(searchDir string) {
			defer func() {
				if r := recover(); r != nil {
					// 忽略panic，继续搜索其他目录
				}
			}()

			filepath.WalkDir(searchDir, func(path string, d fs.DirEntry, err error) error {
				if atomic.LoadInt32(&found) == 1 {
					return filepath.SkipAll // 已找到，跳过剩余文件
				}

				if err != nil {
					return nil // 跳过错误
				}

				if !d.IsDir() && strings.EqualFold(filepath.Base(path), targetFile) {
					// 找到第一个文件
					if atomic.CompareAndSwapInt32(&found, 0, 1) {
						select {
						case resultChan <- path:
						default:
						}
						close(done)
					}
					return filepath.SkipAll
				}

				return nil
			})
		}(dir)
	}

	select {
	case result := <-resultChan:
		return result, nil
	case <-done:
		select {
		case result := <-resultChan:
			return result, nil
		default:
			return "", fmt.Errorf("未找到 %s", targetFile)
		}
	}
}
