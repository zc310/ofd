package parser

import (
	"errors"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/zc310/ofd/internal/core"
	"github.com/zc310/ofd/internal/models"
)

func TestReadPagePhysicalBoxMetadataBoundaries(t *testing.T) {
	for _, tc := range []struct {
		name    string
		xml     string
		want    models.StBox
		wantErr bool
	}{
		{"prefixed_area", `<ofd:Page xmlns:ofd="http://www.ofdspec.org/2016"><ofd:Area><ofd:PhysicalBox>1 2 100 200</ofd:PhysicalBox></ofd:Area></ofd:Page>`, models.StBox{X: 1, Y: 2, Width: 100, Height: 200}, false},
		{"no_area", `<Page><Content/></Page>`, models.StBox{}, false},
		{"empty_page", `<Page/>`, models.StBox{}, false},
		{"missing_physical_box", `<Page><Area/></Page>`, models.StBox{}, false},
		{"skip_other_elements", `<Page><Template TemplateID="1"/><Area><PhysicalBox>0 0 100 200</PhysicalBox></Area></Page>`, models.StBox{Width: 100, Height: 200}, false},
		{"skip_content", `<Page><Content><broken`, models.StBox{}, false},
		{"truncated_area", `<Page><Area><PhysicalBox>0 0 100 200`, models.StBox{}, true},
		{"invalid_physical_box", `<Page><Area><PhysicalBox>invalid</PhysicalBox></Area></Page>`, models.StBox{}, true},
		{"limit_before_root", strings.Repeat(" ", maxPageMetadataXMLBytes+2) + `<Page/>`, models.StBox{}, true},
		{"limit_before_area", `<Page>` + strings.Repeat(" ", maxPageMetadataXMLBytes+2) + `<Area/></Page>`, models.StBox{}, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			fc, err := core.OpenBytes(parserTestArchive(t, map[string]string{"Page.xml": tc.xml}))
			if err != nil {
				t.Fatal(err)
			}
			defer fc.Close()
			got, err := readPagePhysicalBox(fc, "Page.xml")
			if (err != nil) != tc.wantErr || got != tc.want {
				t.Fatalf("尺寸元数据 = %+v, %v，期望 %+v，报错=%v", got, err, tc.want, tc.wantErr)
			}
		})
	}
}

func TestReadPagePhysicalBoxReportsUnavailablePackage(t *testing.T) {
	if _, err := readPagePhysicalBox(nil, "Page.xml"); err == nil {
		t.Fatal("包为空时应报错")
	}
	fc, err := core.OpenBytes(parserTestArchive(t, map[string]string{"other.xml": `<Other/>`}))
	if err != nil {
		t.Fatal(err)
	}
	defer fc.Close()
	if _, err := readPagePhysicalBox(fc, "Page.xml"); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("应保留文件不存在错误，实际为 %v", err)
	}
}

func TestPhysicalBoxMetadataCachesConcurrentResults(t *testing.T) {
	for _, wantErr := range []error{nil, errors.New("metadata failed")} {
		name := "success"
		if wantErr != nil {
			name = "error"
		}
		t.Run(name, func(t *testing.T) {
			var calls atomic.Int32
			want := models.StBox{Width: 100, Height: 200}
			page := &Page{metadata: func() (models.StBox, error) {
				calls.Add(1)
				return want, wantErr
			}}
			var workers sync.WaitGroup
			for range 16 {
				workers.Go(func() {
					if got, err := page.PhysicalBoxMetadata(); got != want || err != wantErr {
						t.Errorf("元数据 = %+v, %v，期望 %+v, %v", got, err, want, wantErr)
					}
				})
			}
			workers.Wait()
			if got := calls.Load(); got != 1 {
				t.Fatalf("元数据读取次数 = %d，期望 1", got)
			}
			if page.loaded || page.pinCount.Load() != 0 {
				t.Fatal("元数据读取不应加载或固定页面正文")
			}
		})
	}
}

func TestPageLoadFailureIsCachedWithoutLeakingLease(t *testing.T) {
	wantErr := errors.New("page failed")
	for _, standalone := range []bool{false, true} {
		name := "document"
		if standalone {
			name = "standalone"
		}
		t.Run(name, func(t *testing.T) {
			loads := 0
			page := &Page{load: func(*Page) error {
				loads++
				return wantErr
			}}
			if !standalone {
				page.document = &Document{Pages: []*Page{page}}
			}
			for range 3 {
				if lease, err := page.AcquireLease(); lease != nil || err != wantErr {
					t.Fatalf("加载失败 = %v, %v，期望 nil, %v", lease, err, wantErr)
				}
				if err := page.EnsureLoaded(); err != wantErr {
					t.Fatalf("应保留加载错误，实际为 %v", err)
				}
			}
			if loads != 1 || page.pinCount.Load() != 0 {
				t.Fatalf("加载次数 = %d，租约数 = %d，期望 1, 0", loads, page.pinCount.Load())
			}
			if !standalone {
				page.document.clearCaches()
				if _, err := page.AcquireLease(); err == nil {
					t.Fatal("关闭文档后不应再获取页面租约")
				}
			}
		})
	}
}
