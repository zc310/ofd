package canvas

import (
	"errors"
	"os"
	"testing"
)

// Legacy exact-metric expectations belong to DejaVu Serif, not Go Regular.
// The font is optional and external; no font file is bundled with this fork.
func externalMetricFont(t *testing.T) string {
	t.Helper()
	path := os.Getenv("OFD_DEJAVU_SERIF_FONT")
	if path == "" {
		t.Skip("set OFD_DEJAVU_SERIF_FONT for legacy exact-metric tests")
	}
	if _, err := os.Stat(path); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			t.Skipf("missing external metric font: %s", path)
		}
		t.Fatal(err)
	}
	return path
}
