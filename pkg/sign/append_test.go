package sign

import (
	"errors"
	"fmt"
	"io"
	"testing"

	"github.com/zc310/ofd/internal/core"
	protocol "github.com/zc310/ofd/internal/crypt"
)

func TestAppendBudgetsRejectContainerBeforeReadingPayload(t *testing.T) {
	for _, mode := range []string{"entry", "total", "count"} {
		t.Run(mode, func(t *testing.T) {
			count, size := 1, uint64((64<<20)+1)
			if mode == "total" {
				count, size = 9, 64<<20
			}
			if mode == "count" {
				count, size = 10001, 0
			}
			infos := make([]core.Entry, count)
			for n := range infos {
				infos[n] = core.Entry{Path: fmt.Sprintf("entry-%d", n), UncompressedSize: size}
			}
			opened := false
			pkg, err := core.NewVirtual(infos, func(string) (io.ReadCloser, error) {
				opened = true
				return nil, errors.New("不得读取负载")
			}, nil)
			if err != nil {
				t.Fatal(err)
			}
			defer pkg.Close()
			if _, err := readAppendEntries(t.Context(), pkg); !errors.Is(err, protocol.ErrLimitExceeded) || opened {
				t.Fatalf("预算未在读取前拒绝: %v opened=%v", err, opened)
			}
		})
	}
}
