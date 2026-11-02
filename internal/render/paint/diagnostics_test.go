package paint

import (
	"errors"
	"testing"

	"github.com/zc310/ofd/internal/models"
)

type brokenProfileResources struct {
	testResources

	reads int
}

func (r *brokenProfileResources) ReadColorProfile(string) ([]byte, error) {
	r.reads++
	return nil, errors.New("读取颜色配置失败")
}

func TestColorProfileFailureIsReplayedAfterCacheHit(t *testing.T) {
	r := &brokenProfileResources{testResources: testResources{7: {Type: "RGB", Profile: "missing.icc"}}}
	var resolver Resolver
	defer resolver.Close()
	color := &models.CTColor{ColorSpace: 7}
	resolver.Resolve(color, r)
	for range 2 {
		issues := 0
		resolver.DiagnoseColors(r, func(code, message string, id models.StID) {
			issues++
			if code != "color_profile_read_failed" || id != 7 || message == "" {
				t.Fatalf("颜色诊断缺少稳定 code 或资源 ID：%s %d", code, id)
			}
		}, color)
		if issues != 1 {
			t.Fatal("颜色缓存命中丢失本次诊断")
		}
	}
	if r.reads != 1 {
		t.Fatal("收集报告重复读取 Profile")
	}
}
