package drawing

import (
	"testing"

	"github.com/zc310/ofd/internal/render/geom"
)

func TestBackendNamesOnlyListsRegisteredBackendsOnce(t *testing.T) {
	backendMu.Lock()
	previous := backendByName
	backendByName = map[string]BackendFactory{}
	backendMu.Unlock()
	t.Cleanup(func() {
		backendMu.Lock()
		backendByName = previous
		backendMu.Unlock()
	})
	if len(BackendNames()) != 0 {
		t.Fatal("未注册时不应提示 canvas")
	}
	if err := RegisterBackend(BackendCanvas, func(float64, float64, geom.Resolution) (Backend, error) { return nil, errNilBackendFactory }); err != nil {
		t.Fatal(err)
	}
	for _, name := range BackendNames() {
		if _, ok := LookupBackend(name); !ok {
			t.Fatalf("提示了未注册后端 %q", name)
		}
	}
	seen := make(map[string]bool)
	for _, name := range BackendNames() {
		if seen[name] {
			t.Fatalf("后端名重复：%q", name)
		}
		seen[name] = true
	}
}
