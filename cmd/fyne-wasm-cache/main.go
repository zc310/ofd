package main

import (
	"crypto/sha256"
	"fmt"
	"os"
	"strings"
)

const wasmFetch = `fetch("ofd-viewer.wasm")`

func main() {
	if len(os.Args) != 3 {
		fmt.Fprintln(os.Stderr, "usage: fyne-wasm-cache <index.html> <ofd-viewer.wasm>")
		os.Exit(2)
	}
	if err := addWASMCacheKey(os.Args[1], os.Args[2]); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func addWASMCacheKey(indexPath, wasmPath string) error {
	index, err := os.ReadFile(indexPath)
	if err != nil {
		return fmt.Errorf("read Fyne web index: %w", err)
	}
	if strings.Count(string(index), wasmFetch) != 1 {
		return fmt.Errorf("Fyne web index must contain exactly one %s reference", wasmFetch)
	}
	wasm, err := os.ReadFile(wasmPath)
	if err != nil {
		return fmt.Errorf("read Fyne WebAssembly file: %w", err)
	}
	hash := sha256.Sum256(wasm)
	versionedFetch := fmt.Sprintf(`fetch("ofd-viewer.wasm?v=%x")`, hash[:8])
	index = []byte(strings.Replace(string(index), wasmFetch, versionedFetch, 1))
	info, err := os.Stat(indexPath)
	if err != nil {
		return fmt.Errorf("stat Fyne web index: %w", err)
	}
	if err := os.WriteFile(indexPath, index, info.Mode().Perm()); err != nil {
		return fmt.Errorf("write cache-busted Fyne web index: %w", err)
	}
	return nil
}
