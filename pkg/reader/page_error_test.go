package reader

import (
	"bytes"
	"errors"
	"io"
	"testing"

	"github.com/klauspost/compress/zip"
)

func malformedPagePackage(t *testing.T) []byte {
	t.Helper()
	entries := map[string]string{
		"OFD.xml":          `<OFD Version="1.0"><DocBody><DocRoot>Doc/Document.xml</DocRoot></DocBody></OFD>`,
		"Doc/Document.xml": `<Document><CommonData><PageArea><PhysicalBox>0 0 20 20</PhysicalBox></PageArea></CommonData><Pages><Page ID="1" BaseLoc="Page.xml"/></Pages></Document>`,
		"Doc/Page.xml":     `<Page><Content><Layer ID="1">`,
	}
	var data bytes.Buffer
	archive := zip.NewWriter(&data)
	for name, content := range entries {
		entry, err := archive.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := entry.Write([]byte(content)); err != nil {
			t.Fatal(err)
		}
	}
	if err := archive.Close(); err != nil {
		t.Fatal(err)
	}
	return data.Bytes()
}

func TestMalformedPageErrorsAreNotReportedAsEmptyContent(t *testing.T) {
	r, err := Open(malformedPagePackage(t))
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()

	checks := []struct {
		name string
		call func() error
	}{
		{name: "Text", call: func() error { _, err := r.Text(0); return err }},
		{name: "Search", call: func() error { _, err := r.Search(t.Context(), "x", SearchOptions{}); return err }},
		{name: "FontUsage", call: func() error { _, err := r.FontUsage(FontRef{Scope: 0, ID: 1}, FontUsageOptions{}); return err }},
		{name: "PageLinks", call: func() error { _, err := r.PageLinks(); return err }},
		{name: "PageLinksForPage", call: func() error { _, err := r.PageLinksForPage(0); return err }},
		{name: "PageMediaActions", call: func() error { _, err := r.PageMediaActions(); return err }},
		{name: "PageMediaActionsForPage", call: func() error { _, err := r.PageMediaActionsForPage(0); return err }},
		{name: "PageEntryActionsForPage", call: func() error { _, err := r.PageEntryActionsForPage(0); return err }},
	}
	for _, check := range checks {
		t.Run(check.name, func(t *testing.T) {
			err := check.call()
			if err == nil {
				t.Fatal("损坏页面被当作空内容")
			}
			if !errors.Is(err, io.EOF) {
				t.Fatalf("错误未保留底层 EOF 原因: %v", err)
			}
		})
	}
}
