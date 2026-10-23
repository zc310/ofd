package parser

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/zc310/ofd/pkg/crypt"
)

func TestEncryptedPackageReportsLockedBeforeReadingOFD(t *testing.T) {
	files := map[string]string{"Cipher/root.dat": strings.Repeat("x", 32)}
	for name, source := range map[string]string{"Encryptions.xml": "Encryptions.xml", "Security/seed.xml": "seed.xml", "Security/map.dat": "map.xml"} {
		data, err := os.ReadFile(filepath.Join("..", "..", "testdata", "crypt", source))
		if errors.Is(err, os.ErrNotExist) {
			t.Skipf("missing crypto fixture %s", source)
		}
		if err != nil {
			t.Fatal(err)
		}
		files[name] = string(data)
	}
	for _, placeholder := range []string{"", "not XML", `<OFD xmlns="http://www.ofdspec.org/2016"/>`} {
		if placeholder == "" {
			delete(files, "OFD.xml")
		} else {
			files["OFD.xml"] = placeholder
		}
		ofd, err := NewOFD(parserTestArchive(t, files))
		if ofd != nil {
			defer ofd.Close()
		}
		var locked *crypt.LockedError
		if !errors.Is(err, crypt.ErrLocked) || !errors.As(err, &locked) || locked.Cause != nil || locked.Info.State != "locked" {
			t.Fatalf("placeholder %q: %v", placeholder, err)
		}
	}
}

func TestAmbiguousEncryptionMarkerCannotFallThroughToPlaintextParser(t *testing.T) {
	files := map[string]string{"OFD.xml": `<OFD xmlns="http://www.ofdspec.org/2016"/>`, "ENCRYPTIONS.XML": "invalid", "encryptions.xml": "invalid"}
	ofd, err := NewOFD(parserTestArchive(t, files))
	if ofd != nil {
		defer ofd.Close()
	}
	if !errors.Is(err, crypt.ErrLocked) || !errors.Is(err, crypt.ErrInvalidMetadata) {
		t.Fatalf("ambiguous marker error = %v", err)
	}
}
