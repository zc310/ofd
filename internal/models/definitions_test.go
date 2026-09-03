package models

import (
	"encoding/xml"
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
