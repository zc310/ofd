package models

import (
	"encoding/xml"
	"testing"
)

func TestAttachmentDateTimeAttributes(t *testing.T) {
	var attachment Attachment
	err := xml.Unmarshal([]byte(`<Attachment ID="1" Name="invoice.xml" CreationDate="2020-01-02" ModDate="2020-01-02T03:04:05Z"><FileLoc>invoice.xml</FileLoc></Attachment>`), &attachment)
	if err != nil {
		t.Fatal(err)
	}
	if attachment.CreationDate.IsZero() || attachment.CreationDate.Format("2006-01-02") != "2020-01-02" {
		t.Fatalf("creation date = %v", attachment.CreationDate)
	}
	if attachment.ModDate.IsZero() || attachment.ModDate.Format("2006-01-02T15:04:05Z07:00") != "2020-01-02T03:04:05Z" {
		t.Fatalf("mod date = %v", attachment.ModDate)
	}
}

func TestAttachmentDateTimeAttributesMayBeOmitted(t *testing.T) {
	var attachment Attachment
	if err := xml.Unmarshal([]byte(`<Attachment ID="1" Name="invoice.xml"><FileLoc>invoice.xml</FileLoc></Attachment>`), &attachment); err != nil {
		t.Fatal(err)
	}
	if !attachment.CreationDate.IsZero() || !attachment.ModDate.IsZero() {
		t.Fatalf("dates = creation %v, modification %v", attachment.CreationDate, attachment.ModDate)
	}
}
