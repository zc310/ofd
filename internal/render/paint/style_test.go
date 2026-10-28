package paint

import (
	"math"
	"reflect"
	"testing"

	"github.com/zc310/ofd/internal/models"
	"github.com/zc310/ofd/internal/render/geom"
)

func TestValidDashPattern(t *testing.T) {
	pattern := models.StArrayF{2, 1}
	if !ValidDashPattern(0, &pattern) {
		t.Fatal("有效虚线被拒绝")
	}
	for _, invalid := range []models.StArrayF{{-1, 1}, {0, 0}} {
		if ValidDashPattern(0, &invalid) {
			t.Fatalf("非法虚线被接受: %v", invalid)
		}
	}
}

func TestResolvePathStyleInheritsWithoutMutatingSharedResources(t *testing.T) {
	pattern := models.StArrayF{0, 3}
	fill, stroke := &models.CTColor{Index: 1}, &models.CTColor{Index: 2}
	dp := &models.DrawParam{
		LineWidth: 2, Cap: "Round", Join: "Miter", MiterLimit: 4,
		DashOffset: 1, DashPattern: &pattern, FillColor: fill, StrokeColor: stroke,
	}
	object := models.CtPath{FillColor: &models.CTColor{}}
	effective := ResolvePathStyle(object, dp, 2)
	if effective.LineWidth != 4 || effective.MiterLimit != 8 || effective.DashOffset != 2 {
		t.Fatalf("继承的描边长度未正确缩放: %+v", effective.CTGraphicUnit)
	}
	if effective.Cap != "Round" || effective.Join != "Miter" || effective.FillColor != fill || effective.StrokeColor != stroke {
		t.Fatal("未按就近原则继承有效属性和颜色")
	}
	if effective.DashPattern == nil || !reflect.DeepEqual(*effective.DashPattern, models.StArrayF{0.001, 6}) {
		t.Fatalf("圆端点点划未保留: %v", effective.DashPattern)
	}
	if !reflect.DeepEqual(pattern, models.StArrayF{0, 3}) || dp.LineWidth != 2 || object.DashPattern != nil || object.LineWidth != 0 {
		t.Fatal("解析修改了共享资源或输入对象")
	}
	joiner, ok := StrokeJoiner(effective.Join, effective.MiterLimit, effective.LineWidth).(geom.MiterJoiner)
	if !ok || joiner.Limit != 4 {
		t.Fatalf("绝对毫米斜接限制换算错误: %v", joiner)
	}
}

func TestResolvePathStyleObjectDashAndTransparentColorOverrideInheritedValues(t *testing.T) {
	pattern := models.StArrayF{1, 2}
	transparent := &models.CTColor{Value: &models.Color{}}
	object := models.CtPath{
		CTGraphicUnit: models.CTGraphicUnit{
			LineWidth: 3, Cap: "Butt", Join: "Bevel", MiterLimit: 5, DashPattern: &pattern,
		},
		FillColor: transparent,
	}
	effective := ResolvePathStyle(object, &models.DrawParam{
		LineWidth: 7, Cap: "Round", Join: "Round", MiterLimit: 9,
		DashOffset: 4, DashPattern: &models.StArrayF{6, 8}, FillColor: &models.CTColor{Index: 1},
	}, 1)
	if effective.LineWidth != 3 || effective.Cap != "Butt" || effective.Join != "Bevel" || effective.MiterLimit != 5 {
		t.Fatal("对象属性未覆盖继承属性")
	}
	if effective.DashOffset != 0 || effective.DashPattern != &pattern || effective.FillColor != transparent {
		t.Fatal("显式虚线的零偏移或透明颜色被继承值覆盖")
	}
}

func TestResolvePathStyleKeepsFallbacksForInvalidLengths(t *testing.T) {
	for _, value := range []float64{-1, math.NaN(), math.Inf(1), math.MaxFloat64} {
		effective := ResolvePathStyle(models.CtPath{CTGraphicUnit: models.CTGraphicUnit{
			LineWidth: value, MiterLimit: value,
		}}, nil, 1)
		if effective.LineWidth != DefaultLineWidth || effective.MiterLimit != defaultMiterLimit {
			t.Fatalf("非法长度未回退: %v", effective.CTGraphicUnit)
		}
	}
}
