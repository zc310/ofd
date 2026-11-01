package spec_test

import (
	"encoding/xml"
	"reflect"
	"testing"
	"time"

	"github.com/zc310/ofd/pkg/spec"
)

func TestPermissionsXMLPreservesOmissionFalseAndEmptyPeriod(t *testing.T) {
	for _, source := range []string{`<Permissions/>`, `<Permissions><Print/></Permissions>`} {
		var value spec.Permissions
		if err := xml.Unmarshal([]byte(source), &value); err != nil {
			t.Fatal(err)
		}
		if value.Edit != nil || value.Annot != nil || value.Export != nil || value.Signature != nil || value.Watermark != nil || value.PrintScreen != nil || value.ValidPeriod != nil {
			t.Fatalf("省略字段被填充：%+v", value)
		}
		if value.Print != nil && (!value.Print.Printable || value.Print.Copies != -1) {
			t.Fatalf("打印属性缺省值错误：%+v", value.Print)
		}
	}
	var value spec.Permissions
	if err := xml.Unmarshal([]byte(`<Permissions><Edit>false</Edit><ValidPeriod/></Permissions>`), &value); err != nil {
		t.Fatal(err)
	}
	if value.Edit == nil || *value.Edit || value.ValidPeriod == nil || !value.ValidPeriod.StartDate.IsZero() || !value.ValidPeriod.EndDate.IsZero() {
		t.Fatalf("显式false或空期限丢失：%+v", value)
	}
}

func TestPermissionsXMLRoundTripPreservesSingleEndpointsAndPrecision(t *testing.T) {
	for _, source := range []string{
		`<Permissions><Export>false</Export><Print Printable="false" Copies="0"/><ValidPeriod StartDate="2026-01-02T03:04:05.123456789+08:00"/></Permissions>`,
		`<Permissions><Print Copies="2"/><ValidPeriod EndDate="2026-01-02T03:04:05.123456789Z"/></Permissions>`,
		`<Permissions><ValidPeriod/></Permissions>`,
		`<Permissions/>`,
	} {
		var before, after spec.Permissions
		if err := xml.Unmarshal([]byte(source), &before); err != nil {
			t.Fatal(err)
		}
		data, err := xml.Marshal(before)
		if err != nil {
			t.Fatal(err)
		}
		if err := xml.Unmarshal(data, &after); err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(before, after) {
			t.Fatalf("权限往返丢失信息：%s\n前=%+v 后=%+v", data, before, after)
		}
	}
}

func TestPermissionsCloneDoesNotShareMutableFields(t *testing.T) {
	if (*spec.Permissions)(nil).Clone() != nil {
		t.Fatal("nil声明克隆后不再是nil")
	}
	allowed := true
	date := spec.DateTime{Time: time.Date(2026, 1, 2, 0, 0, 0, 1, time.UTC)}
	value := &spec.Permissions{Edit: &allowed, Annot: &allowed, Export: &allowed, Signature: &allowed, Watermark: &allowed, PrintScreen: &allowed,
		Print: &spec.PrintSettings{Printable: true, Copies: -1}, ValidPeriod: &spec.ValidPeriod{StartDate: date}}
	copyValue := value.Clone()
	if !reflect.DeepEqual(copyValue, value) {
		t.Fatal("克隆改变了原始声明")
	}
	for _, field := range []*bool{copyValue.Edit, copyValue.Annot, copyValue.Export, copyValue.Signature, copyValue.Watermark, copyValue.PrintScreen} {
		*field = false
	}
	copyValue.Print.Copies = 0
	copyValue.ValidPeriod.StartDate = spec.DateTime{}
	if !allowed || value.Print.Copies != -1 || !value.ValidPeriod.StartDate.Equal(date.Time) {
		t.Fatal("修改副本影响了原始声明")
	}
	if got := (&spec.Permissions{}).Clone(); !reflect.DeepEqual(got, &spec.Permissions{}) {
		t.Fatal("克隆填充了未声明字段")
	}
}
