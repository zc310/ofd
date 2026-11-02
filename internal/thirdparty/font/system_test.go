package font

import (
	"os"
	"path/filepath"
	"testing"
)

// TestGetSFNTMetadataUsesOS2Weight prevents Arial Black from being classified as
// regular Arial. ariblk.ttf declares a Regular subfamily; its OS/2 usWeightClass
// of 900 distinguishes it from regular Arial at 400. Misclassification would
// replace regular Arial with wider glyphs and cause overlapping text.
func TestGetSFNTMetadataUsesOS2Weight(t *testing.T) {
	black := findSystemFontFile("ariblk.ttf")
	regular := findSystemFontFile("arial.ttf")
	if black == "" || regular == "" {
		t.Skip("Arial/Arial Black 未安装")
	}

	blackMeta := readSFNTMetadata(t, black)
	if got := blackMeta.Style.Weight(); got != Black {
		t.Fatalf("Arial Black weight = %v, want Black", got)
	}
	regularMeta := readSFNTMetadata(t, regular)
	if got := regularMeta.Style.Weight(); got != Regular {
		t.Fatalf("Arial weight = %v, want Regular", got)
	}
	if blackMeta.Style == regularMeta.Style {
		t.Fatalf("Arial Black and Arial map to the same style %v", blackMeta.Style)
	}
}

func readSFNTMetadata(t *testing.T, path string) FontMetadata {
	t.Helper()
	file, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	metadata, err := getSFNTMetadata(file)
	if err != nil {
		t.Fatalf("%s: %v", path, err)
	}
	return metadata
}

func findSystemFontFile(name string) string {
	for _, dir := range DefaultFontDirs() {
		var found string
		_ = filepath.WalkDir(dir, func(path string, entry os.DirEntry, err error) error {
			if err != nil || entry.IsDir() || found != "" {
				return nil
			}
			if entry.Name() == name {
				found = path
			}
			return nil
		})
		if found != "" {
			return found
		}
	}
	return ""
}
