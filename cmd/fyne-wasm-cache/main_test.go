package main

import (
	"crypto/sha256"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestAddWASMCacheKeyTracksBinaryContent(t *testing.T) {
	dir := t.TempDir()
	indexPath := filepath.Join(dir, "index.html")
	wasmPath := filepath.Join(dir, "ofd-viewer.wasm")
	index := `<script>fetch("ofd-viewer.wasm")</script>`
	if err := os.WriteFile(indexPath, []byte(index), 0o600); err != nil {
		t.Fatal(err)
	}
	wasm := []byte("wasm build one")
	if err := os.WriteFile(wasmPath, wasm, 0o600); err != nil {
		t.Fatal(err)
	}

	if err := addWASMCacheKey(indexPath, wasmPath); err != nil {
		t.Fatal(err)
	}
	first, err := os.ReadFile(indexPath)
	if err != nil {
		t.Fatal(err)
	}
	hash := sha256.Sum256(wasm)
	want := fmt.Sprintf(`fetch("ofd-viewer.wasm?v=%x")`, hash[:8])
	if !strings.Contains(string(first), want) {
		t.Fatalf("versioned URL missing: %s", first)
	}

	if err := os.WriteFile(indexPath, []byte(index), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(wasmPath, []byte("wasm build two"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := addWASMCacheKey(indexPath, wasmPath); err != nil {
		t.Fatal(err)
	}
	second, err := os.ReadFile(indexPath)
	if err != nil {
		t.Fatal(err)
	}
	if string(first) == string(second) {
		t.Fatal("changed WASM content should change the download URL")
	}
}

func TestAddWASMCacheKeyRejectsUnexpectedIndex(t *testing.T) {
	dir := t.TempDir()
	indexPath := filepath.Join(dir, "index.html")
	wasmPath := filepath.Join(dir, "ofd-viewer.wasm")
	if err := os.WriteFile(indexPath, []byte("missing fetch"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(wasmPath, []byte("wasm"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := addWASMCacheKey(indexPath, wasmPath); err == nil {
		t.Fatal("expected an error when the Fyne WASM fetch reference changes")
	}
}
