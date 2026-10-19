//go:build js && wasm

package main

import (
	"context"
	"errors"
	"syscall/js"
	"time"

	"github.com/hack-pad/go-indexeddb/idb"
	"github.com/hack-pad/safejs"
)

const (
	fallbackFontDatabase = "ofd-viewer-fonts"
	fallbackFontStore    = "fonts"
	fallbackFontCacheKey = "Noto Sans SC:400"
)

type indexedDBFontCache struct{}

func newFallbackFontCache() fallbackFontCache { return indexedDBFontCache{} }

func openFontDatabase(ctx context.Context) (*idb.Database, error) {
	indexedDB, err := safejs.Global().Get("indexedDB")
	if err != nil {
		return nil, err
	}
	available, err := indexedDB.Truthy()
	if err != nil || !available {
		return nil, errors.New("IndexedDB is unavailable")
	}
	factory, err := idb.WrapFactory(safejs.Unsafe(indexedDB))
	if err != nil {
		return nil, err
	}
	request, err := factory.Open(ctx, fallbackFontDatabase, 1, func(db *idb.Database, _, _ uint) error {
		_, err := db.CreateObjectStore(fallbackFontStore, idb.ObjectStoreOptions{})
		return err
	})
	if err != nil {
		return nil, err
	}
	return request.Await(ctx)
}

func (indexedDBFontCache) Load(ctx context.Context) ([]byte, error) {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	db, err := openFontDatabase(ctx)
	if err != nil {
		return nil, err
	}
	defer db.Close()
	transaction, err := db.Transaction(idb.TransactionReadOnly, fallbackFontStore)
	if err != nil {
		return nil, err
	}
	store, err := transaction.ObjectStore(fallbackFontStore)
	if err != nil {
		return nil, err
	}
	request, err := store.Get(js.ValueOf(fallbackFontCacheKey))
	if err != nil {
		return nil, err
	}
	value, err := request.Await(ctx)
	if err != nil {
		return nil, err
	}
	if value.IsUndefined() || value.IsNull() {
		return nil, nil
	}
	if !value.InstanceOf(js.Global().Get("Uint8Array")) {
		return nil, errors.New("Invalid cached font data")
	}
	length := value.Get("byteLength").Int()
	if length == 0 || length > maxFallbackFontBytes {
		return nil, errors.New(tr("回退字体文件为空或超过 32 MiB"))
	}
	data := make([]byte, length)
	js.CopyBytesToGo(data, value)
	return data, nil
}

func (indexedDBFontCache) Store(ctx context.Context, data []byte) error {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	db, err := openFontDatabase(ctx)
	if err != nil {
		return err
	}
	defer db.Close()
	transaction, err := db.Transaction(idb.TransactionReadWrite, fallbackFontStore)
	if err != nil {
		return err
	}
	defer transaction.Abort()
	store, err := transaction.ObjectStore(fallbackFontStore)
	if err != nil {
		return err
	}
	value := js.Global().Get("Uint8Array").New(len(data))
	js.CopyBytesToJS(value, data)
	if _, err := store.PutKey(js.ValueOf(fallbackFontCacheKey), value); err != nil {
		return err
	}
	// Wait for the transaction to commit, not just the individual put request.
	return transaction.Await(ctx)
}
