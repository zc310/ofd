package models

import (
	"encoding/xml"
	"image/color"
	"math"
	"testing"
)

func TestStIDUnmarshalXMLInvalidValueUsesZero(t *testing.T) {
	var id StID = 42
	if err := xml.Unmarshal([]byte("<ID>invalid</ID>"), &id); err != nil {
		t.Fatal(err)
	}
	if id != 0 {
		t.Fatalf("StID = %d, want 0", id)
	}
}

func TestStIDUnmarshalXMLAttrInvalidValueUsesZero(t *testing.T) {
	var document struct {
		ID StID `xml:"ID,attr"`
	}
	if err := xml.Unmarshal([]byte(`<Document ID="invalid"/>`), &document); err != nil {
		t.Fatal(err)
	}
	if document.ID != 0 {
		t.Fatalf("StID = %d, want 0", document.ID)
	}
}

func TestStRefIDUnmarshalXMLAttrInvalidValueUsesZero(t *testing.T) {
	var document struct {
		RefID StRefID `xml:"RefID,attr"`
	}
	if err := xml.Unmarshal([]byte(`<Document RefID="invalid"/>`), &document); err != nil {
		t.Fatal(err)
	}
	if document.RefID != 0 {
		t.Fatalf("StRefID = %d, want 0", document.RefID)
	}
}

func TestCTMUnmarshalXMLAttrRejectsNonFiniteValues(t *testing.T) {
	for _, value := range []string{"NaN", "+Inf", "-Inf"} {
		t.Run(value, func(t *testing.T) {
			var document struct {
				CTM CTM `xml:"CTM,attr"`
			}
			if err := xml.Unmarshal([]byte(`<Document CTM="1 0 0 1 `+value+` 0"/>`), &document); err == nil {
				t.Fatal("expected non-finite CTM value to be rejected")
			}
		})
	}
}

func TestCTMIsFinite(t *testing.T) {
	finite := CTM{1, 0, 0, 1, 10, 20}
	if !finite.IsFinite() {
		t.Fatal("finite CTM reported as invalid")
	}
	nonFinite := CTM{1, 0, 0, 1, math.NaN(), 0}
	if nonFinite.IsFinite() {
		t.Fatal("non-finite CTM reported as valid")
	}
}

func TestStBoxRejectsNonFiniteValues(t *testing.T) {
	for _, value := range []string{"NaN", "+Inf", "-Inf"} {
		t.Run(value, func(t *testing.T) {
			var box StBox
			if err := box.parseFromString("0 0 " + value + " 10"); err == nil {
				t.Fatal("expected non-finite StBox value to be rejected")
			}
		})
	}
}

func TestStBoxIsFinite(t *testing.T) {
	if !(StBox{X: 1, Y: 2, Width: 3, Height: 4}).IsFinite() {
		t.Fatal("finite StBox reported as invalid")
	}
	if (StBox{X: 1, Y: 2, Width: 3, Height: math.Inf(1)}).IsFinite() {
		t.Fatal("non-finite StBox reported as valid")
	}
}

func TestStPosRejectsNonFiniteValues(t *testing.T) {
	for _, value := range []string{"NaN", "+Inf", "-Inf"} {
		t.Run(value, func(t *testing.T) {
			var position StPos
			if err := position.parseFromString(value + " 10"); err == nil {
				t.Fatal("expected non-finite position to be rejected")
			}
		})
	}
}

func TestCtPageAreaEnsurePhysicalBoxUsesA4(t *testing.T) {
	tests := []StBox{
		{},
		{Width: 210},
		{Height: 297},
		{Width: -1, Height: 297},
		{Width: 210, Height: -1},
	}
	for _, box := range tests {
		area := CtPageArea{PhysicalBox: box}
		area.EnsurePhysicalBox()
		if area.PhysicalBox != (StBox{Width: 210, Height: 297}) {
			t.Fatalf("PhysicalBox = %+v, want A4", area.PhysicalBox)
		}
	}
}

func TestPageContentEnsurePhysicalBoxUsesA4(t *testing.T) {
	page := PageContent{}
	page.EnsurePhysicalBox()

	if page.Area == nil {
		t.Fatal("EnsurePhysicalBox() did not initialize Area")
	}
	if page.Area.PhysicalBox != (StBox{Width: 210, Height: 297}) {
		t.Fatalf("PhysicalBox = %+v, want A4", page.Area.PhysicalBox)
	}
}

func TestPageContentEnsurePhysicalBoxPreservesValidBox(t *testing.T) {
	want := StBox{X: 10, Y: 20, Width: 210, Height: 297}
	page := PageContent{Area: &CtPageArea{PhysicalBox: want}}

	page.EnsurePhysicalBox()
	if page.Area.PhysicalBox != want {
		t.Fatalf("PhysicalBox = %+v, want %+v", page.Area.PhysicalBox, want)
	}
}

func TestColorParse(t *testing.T) {
	tests := []struct {
		value    string
		wantRGBA color.RGBA
		wantErr  bool
	}{
		{value: "0 0 0 65535", wantRGBA: color.RGBA{A: 255}},
		{value: "65535 0 0 65535", wantRGBA: color.RGBA{R: 255, A: 255}},
		{value: "257 0 0 65535", wantRGBA: color.RGBA{R: 1, A: 255}},
		{value: "0 0 0 32768", wantRGBA: color.RGBA{A: 128}},
		{value: "156 82 35", wantRGBA: color.RGBA{R: 156, G: 82, B: 35, A: 255}},
		{value: "0 0 0 255", wantRGBA: color.RGBA{A: 255}},
		{value: "70000 0 0 255", wantErr: true},
		{value: "0 0 0", wantRGBA: color.RGBA{A: 255}},
	}
	for _, tc := range tests {
		var c Color
		err := c.parse(tc.value)
		if tc.wantErr {
			if err == nil {
				t.Fatalf("parse(%q) 应报错", tc.value)
			}
			continue
		}
		if err != nil {
			t.Fatalf("parse(%q) 失败: %v", tc.value, err)
		}
		if c.RGBA != tc.wantRGBA {
			t.Fatalf("parse(%q) = %+v, want %+v", tc.value, c.RGBA, tc.wantRGBA)
		}
	}
}
