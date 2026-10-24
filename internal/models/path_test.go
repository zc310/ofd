package models

import (
	"encoding/xml"
	"fmt"
	"math"
	"reflect"
	"strings"
	"testing"
)

func TestParsePathDataRejectsNonFiniteValues(t *testing.T) {
	for _, data := range []string{
		"M NaN 0",
		"L 0 +Inf",
		"A 1 2 NaN 0 0 3 4",
	} {
		t.Run(data, func(t *testing.T) {
			if _, err := ParsePathData(data); err == nil {
				t.Fatal("expected non-finite path value to be rejected")
			}
		})
	}
}

func TestPathScannerMatchesFields(t *testing.T) {
	corpus := []string{
		"",
		"   \t\n ",
		"M 0 0 L 10 10 C",
		"M 0.5 -1.25 10 20 C",
		"  M  1  2  3  4  ",
		"A 5 6 0 1 0 7 8 9 10",
		"M 0 0\nL 1 1\tA 2 3 0\r0 0 4 5 C",
		"中文 0 1 M 2 3",
		"M 0\xc2\xa00 L 1 1 C",
	}
	for _, data := range corpus {
		want := strings.Fields(data)
		scanner := pathScanner{data: data}
		for i := range want {
			if got := scanner.next(); got != want[i] {
				t.Fatalf("scanner(%q)[%d] = %q, want %q", data, i, got, want[i])
			}
		}
		if got := scanner.next(); got != "" {
			t.Fatalf("scanner(%q) has extra token %q", data, got)
		}
	}
	if _, err := ParsePathData("M 10 20 30 40 50 60 C"); err != nil {
		t.Fatalf("implicit coords parse failed: %v", err)
	}
	if _, err := ParsePathData("M 3,3 L 57,3 L 57,21 L 3,21 C"); err != nil {
		t.Fatalf("comma-separated coords parse failed: %v", err)
	}
}

func TestParsePathDataCompactCommandsWithUnicodeWhitespace(t *testing.T) {
	want, err := ParsePathData("M 0 0 L 1 1 C")
	if err != nil {
		t.Fatal(err)
	}
	for _, data := range []string{"M0 0L1 1C", "M0\u00a00L1\u00a01C", "M0\u30000,L1\u20031C"} {
		t.Run(data, func(t *testing.T) {
			got, err := ParsePathData(data)
			if err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(got, want) {
				t.Fatalf("got %#v, want %#v", got, want)
			}
		})
	}
}

func TestPathParsingEntrypointsPreserveCommands(t *testing.T) {
	data := "S 1 2 3 4 L 5 6 Q 7 8 9 10 B 11 12 13 14 15 16 A -5 -6 30 1 0 17 18 C"
	want := SVGPath{
		{Type: MoveTo, Points: []StPos{{1, 2}}},
		{Type: LineTo, Points: []StPos{{3, 4}}},
		{Type: LineTo, Points: []StPos{{5, 6}}},
		{Type: QuadTo, Points: []StPos{{7, 8}, {9, 10}}},
		{Type: CubicBezier, Points: []StPos{{11, 12}, {13, 14}, {15, 16}}},
		{Type: ArcTo, Arc: &ArcData{RX: 5, RY: 6, XAxisRotation: 30, LargeArcFlag: true, EndPoint: StPos{17, 18}}},
		{Type: Close},
	}
	for _, entry := range []string{"direct", "xml", "fastXML"} {
		t.Run(entry, func(t *testing.T) {
			var got SVGPath
			var err error
			switch entry {
			case "direct":
				got, err = ParsePathData(data)
			case "xml":
				err = xml.Unmarshal([]byte("<AbbreviatedData>"+data+"</AbbreviatedData>"), &got)
			case "fastXML":
				err = got.UnmarshalXMLText([]byte(data))
			}
			if err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(got, want) {
				t.Fatalf("got %#v, want %#v", got, want)
			}
		})
	}
}

func TestParsePathDataRejectsIncompleteAndInvalidCommands(t *testing.T) {
	for _, data := range []string{
		"M", "M 1", "M 0 0 1", "M 0 0 L 1", "M 0 0 Q 1 2 3",
		"M 0 0 B 1 2 3 4 5", "M 0 0 A 1 2 0 1 0 3",
		"M 0 0 L C", "M 0 0 X 1 2", "M 0 0 C 1 2",
		"M 0 0 Q 1 2 3 4 5 6", "L 1 2", "Q 1 2 3 4", "B 1 2 3 4 5 6", "A 1 2 0 0 0 3 4",
		"M 0 +Inf", "M 0 0 A 1 2 NaN 0 0 3 4", "M 0 0\xffL 1 1",
	} {
		t.Run(data, func(t *testing.T) {
			if _, err := ParsePathData(data); err == nil {
				t.Fatal("expected invalid path to be rejected")
			}
		})
	}
}

func TestParsePathDataAcceptsSVGZCloseCommand(t *testing.T) {
	for _, data := range []string{"M 0 0 L 10 0 Z", "M0 0L10 0z"} {
		commands, err := ParsePathData(data)
		if err != nil {
			t.Fatalf("SVG 闭合命令 %q 应能解析: %v", data, err)
		}
		if len(commands) != 3 || commands[2].Type != Close {
			t.Fatalf("闭合命令未解析为 Close: %#v", commands)
		}
	}
}

func TestParsedCommandAppendDoesNotOverwriteOtherCommands(t *testing.T) {
	p, err := ParsePathData("M 1 2 L 3 4 L 5 6")
	if err != nil {
		t.Fatal(err)
	}
	p[0].Points = append(p[0].Points, StPos{7, 8})
	if p[1].Points[0] != (StPos{3, 4}) || p[2].Points[0] != (StPos{5, 6}) {
		t.Fatalf("appending to one command changed another: %#v", p)
	}
}

func TestGetStartPointAfterCloseUsesLatestSubpath(t *testing.T) {
	// GB/T 33190—2016 表 36、表 37：C 闭合到分路径起点，并以该点为当前点。
	for _, data := range []string{
		"M 0 0 L 10 0 C L 20 0",
		"M 50 50 L 60 60 C S 0 0 L 10 0 C C L 20 0",
	} {
		p, err := ParsePathData(data)
		if err != nil {
			t.Fatal(err)
		}
		got, err := p.GetStartPoint(len(p) - 1)
		if err != nil || got != (StPos{0, 0}) {
			t.Fatalf("GetStartPoint(%q) = %v, %v; want (0,0)", data, got, err)
		}
	}
	p := SVGPath{{Type: Start, Points: []StPos{{2, 3}}}, {Type: ArcTo, Arc: &ArcData{EndPoint: StPos{4, 5}}}, {Type: Close}, {Type: LineTo}}
	for index, want := range map[int]StPos{1: {2, 3}, 2: {4, 5}, 3: {2, 3}} {
		got, err := p.GetStartPoint(index)
		if err != nil || got != want {
			t.Fatalf("GetStartPoint(%d) = %v, %v; want %v", index, got, err, want)
		}
	}
	for _, index := range []int{-1, 0, len(p)} {
		if _, err := p.GetStartPoint(index); err == nil {
			t.Fatalf("GetStartPoint(%d) should fail", index)
		}
	}
}

func TestPathStringPreservesFixedPrecision(t *testing.T) {
	p, err := ParsePathData("M -1.125 2.375 Q 3 4 5 6 B 7 8 9 10 11 12 A 5 6 30 1 0 13 14 C")
	if err != nil {
		t.Fatal(err)
	}
	want := "M -1.12 2.38 Q 3.00 4.00 5.00 6.00 B 7.00 8.00 9.00 10.00 11.00 12.00 A 5.00 6.00 30.00 1 0 13.00 14.00 C"
	if got := p.String(); got != want {
		t.Fatalf("String() = %q, want %q", got, want)
	}
	if got := (SVGPath{}).String(); got != "" {
		t.Fatalf("empty String() = %q", got)
	}
	for _, value := range []float64{math.MaxFloat64, -math.MaxFloat64, math.Copysign(0, -1), math.Inf(1), math.NaN()} {
		path := SVGPath{{Type: MoveTo, Points: []StPos{{value, value}}}}
		if got, want := path.String(), fmt.Sprintf("M %.2f %.2f", value, value); got != want {
			t.Fatalf("String(%v) = %q, want %q", value, got, want)
		}
	}
}
