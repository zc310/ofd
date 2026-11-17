package pathbuild

import (
	"encoding/xml"
	"math"
	"testing"

	"github.com/zc310/ofd/internal/render/geom"
	"github.com/zc310/ofd/pkg/spec"
)

func TestNewPathSkipsMalformedCommands(t *testing.T) {
	path := New(&spec.CtPath{
		AbbreviatedData: spec.PathData{
			{Type: spec.MoveTo},
			{Type: spec.LineTo},
			{Type: spec.QuadTo, Points: []spec.StPos{{X: 1, Y: 1}}},
			{Type: spec.CubicBezier, Points: []spec.StPos{{X: 1, Y: 1}, {X: 2, Y: 2}}},
			{Type: spec.ArcTo},
			{Type: spec.MoveTo, Points: []spec.StPos{{X: 0, Y: 0}}},
			{Type: spec.LineTo, Points: []spec.StPos{{X: 10, Y: 10}}},
		},
	}, func(point spec.StPos) (float64, float64) {
		return point.X, point.Y
	})
	if path.Empty() {
		t.Fatal("跳过非法命令后应保留有效命令")
	}
	if bounds := path.Bounds(); bounds.X0 != 0 || bounds.Y0 != 0 || bounds.X1 != 10 || bounds.Y1 != 10 {
		t.Fatalf("路径边界 = %+v，期望 (0,0)-(10,10)", bounds)
	}
}

func TestNewPathHandlesNilInputs(t *testing.T) {
	if path := New(nil, func(point spec.StPos) (float64, float64) {
		return point.X, point.Y
	}); !path.Empty() {
		t.Fatal("nil 路径应产生空路径")
	}
	if path := New(&spec.CtPath{}, nil); !path.Empty() {
		t.Fatal("nil 变换应产生空路径")
	}
}

func TestBuildObjectPathSkipsOverflowedCoordinates(t *testing.T) {
	path := Object(spec.PathObject{
		CtPath: spec.CtPath{
			CTGraphicUnit: spec.CTGraphicUnit{
				Boundary: spec.StBox{X: math.MaxFloat64, Y: math.MaxFloat64},
				CTM:      &spec.CTM{1, 0, 0, 1, 0, 0},
			},
			AbbreviatedData: spec.PathData{{
				Type:   spec.MoveTo,
				Points: []spec.StPos{{X: math.MaxFloat64, Y: math.MaxFloat64}},
			}},
		},
	}, math.MaxFloat64, nil)
	if !path.Empty() {
		t.Fatal("变换后坐标溢出的路径应为空")
	}
}

func TestArcCTMTransformsWholeEllipse(t *testing.T) {
	var object spec.PathObject
	if err := xml.Unmarshal([]byte(`<PathObject Boundary="0 0 100 100"><AbbreviatedData>M 10 0 A 10 10 0 0 1 0 10</AbbreviatedData></PathObject>`), &object); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name string
		ctm  spec.CTM
	}{
		{"scale", spec.CTM{2, 0, 0, 2, 0, 0}},
		{"unequal scale", spec.CTM{3, 0, 0, 0.5, 2, 4}},
		{"rotation", spec.CTM{0, 2, -2, 0, 30, 0}},
		{"shear", spec.CTM{1, 0.2, 0.7, 1, 4, 5}},
		{"reflection", spec.CTM{-2, 0, 0, 1, 30, 5}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			object.CTM = &tc.ctm
			path := Object(object, 100, nil)
			// 取原圆上的解析点，变换后必须满足输出椭圆的方程。
			// 直接核对弧参数，避免把 Flatten 自身的近似误差混入断言。
			scanner := path.Scanner()
			found := false
			for scanner.Scan() {
				if scanner.Cmd() != geom.ArcToCmd {
					continue
				}
				found = true
				rx, ry, degrees, large, sweep := scanner.Arc()
				angle := degrees * math.Pi / 180
				for i := 0; i <= 8; i++ {
					theta := float64(i) * math.Pi / 16
					x, y := tc.ctm.Transform(10*math.Cos(theta), 10*math.Sin(theta))
					x, y = x-tc.ctm[4], -(y - tc.ctm[5])
					u, v := (x*math.Cos(angle)+y*math.Sin(angle))/rx, (-x*math.Sin(angle)+y*math.Cos(angle))/ry
					if math.Abs(u*u+v*v-1) > 1e-8 {
						t.Fatalf("变换后的点不在输出椭圆上：u²+v²=%g", u*u+v*v)
					}
				}
				wantSweep := tc.ctm[0]*tc.ctm[3]-tc.ctm[1]*tc.ctm[2] < 0
				if large || sweep != wantSweep {
					t.Fatal("变换丢失扫掠方向")
				}
			}
			if !found {
				t.Fatal("椭圆弧丢失")
			}
			if tc.name == "scale" && !path.Bounds().Equals(geom.Rect{X0: 0, Y0: 80, X1: 20, Y1: 100}) {
				t.Fatalf("放大圆弧边界=%v，期望(0,80)-(20,100)", path.Bounds())
			}
		})
	}
}
