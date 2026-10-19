//go:build js && wasm

package main

import (
	"bytes"
	"context"
	"syscall/js"
	"testing"
)

func requireIndexedDB(t *testing.T) {
	t.Helper()
	if js.Global().Get("indexedDB").IsUndefined() {
		t.Skip("IndexedDB tests require a browser")
	}
}

func TestIndexedDBFontCacheRoundTrip(t *testing.T) {
	requireIndexedDB(t)
	ctx := context.Background()
	cache := indexedDBFontCache{}
	want := []byte{0, 127, 128, 255, 42}
	if err := cache.Store(ctx, want); err != nil {
		t.Fatal(err)
	}
	// Each operation closes its database connection, so this reads a committed
	// record through a fresh connection rather than an in-memory copy.
	got, err := (indexedDBFontCache{}).Load(ctx)
	if err != nil || !bytes.Equal(got, want) {
		t.Fatalf("cached bytes = %v, error = %v", got, err)
	}
	want[0] = 99
	if got[0] != 0 {
		t.Fatal("cache returned a shared buffer")
	}
}

// The browser harness runs this test twice at the same origin, with a page
// reload between phases, to check persistence across WASM instances.
func TestIndexedDBFontCacheSurvivesReload(t *testing.T) {
	requireIndexedDB(t)
	phase := js.Global().Get("location").Get("search").String()
	cache := indexedDBFontCache{}
	want := []byte("persisted fallback font across WASM reloads")
	if phase == "?phase=write" {
		if err := cache.Store(context.Background(), want); err != nil {
			t.Fatal(err)
		}
	} else if phase == "?phase=read" {
		got, err := cache.Load(context.Background())
		if err != nil || !bytes.Equal(got, want) {
			t.Fatalf("persistent font = %q, error = %v", got, err)
		}
	} else {
		t.Skip("cross-reload test needs a write/read phase")
	}
}
