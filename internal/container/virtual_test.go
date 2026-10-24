package container

import (
	"errors"
	"io"
	"os"
	"strings"
	"testing"
)

func TestVirtualPackageEnumerationReadsAndForeignEntryIsolation(t *testing.T) {
	makePackage := func() *Package {
		p, err := NewVirtual([]Entry{{Name: "original", Path: "Doc/data.xml", Virtual: true, UncompressedSize: 7}}, func(name string) (io.ReadCloser, error) {
			if name != "Doc/data.xml" {
				t.Fatal("unexpected normalized path")
			}
			return io.NopCloser(strings.NewReader("content")), nil
		}, nil)
		if err != nil {
			t.Fatal(err)
		}
		return p
	}
	p, other := makePackage(), makePackage()
	defer p.Close()
	defer other.Close()
	entries := p.Entries()
	if len(entries) != 1 || !entries[0].Virtual || entries[0].UncompressedSize != 7 {
		t.Fatalf("entries=%+v", entries)
	}
	entry, ok := p.Lookup("/doc/DATA.xml")
	if !ok {
		t.Fatal("case fallback missing")
	}
	r, err := p.OpenEntry(entry)
	if err != nil {
		t.Fatal(err)
	}
	data, err := io.ReadAll(r)
	r.Close()
	if err != nil || string(data) != "content" {
		t.Fatalf("read=%q %v", data, err)
	}
	if _, err := other.OpenEntry(entry); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("foreign entry accepted")
	}
	var count int
	if err := p.WalkEntries(func(e Entry) bool { count++; return e.Path == "Doc/data.xml" }); err != nil || count != 1 {
		t.Fatalf("walk=%d, %v", count, err)
	}
	if _, err := p.ReadLimit("Doc/data.xml", 6); !errors.Is(err, ErrReadLimitExceeded) {
		t.Fatalf("virtual size budget=%v", err)
	}
	if err := p.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := p.Open("Doc/data.xml"); !errors.Is(err, ErrPackageClosed) {
		t.Fatalf("closed open=%v", err)
	}
}
