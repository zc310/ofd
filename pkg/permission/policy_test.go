package permission

import (
	"encoding/xml"
	"errors"
	"testing"
	"time"

	"github.com/zc310/ofd/pkg/spec"
)

func TestPolicyActionsAreIndependentAndDefaultsAllow(t *testing.T) {
	actions := []Action{Access, Copy, Export, Print, Edit, Annot, Signature, Watermark, PrintScreen}
	for _, d := range []*spec.Permissions{nil, {}} {
		for _, action := range actions {
			if err := (Policy{}).Check(d, action, 3); err != nil {
				t.Fatal(err)
			}
		}
	}
	deniedValue := false
	for _, tc := range []struct {
		action Action
		deny   func(*spec.Permissions)
	}{
		{Copy, func(d *spec.Permissions) { d.Export = &deniedValue }}, {Export, func(d *spec.Permissions) { d.Export = &deniedValue }},
		{Print, func(d *spec.Permissions) { d.Print = &spec.PrintSettings{Printable: false, Copies: -1} }},
		{Print, func(d *spec.Permissions) { d.Print = &spec.PrintSettings{Printable: true, Copies: 0} }},
		{Edit, func(d *spec.Permissions) { d.Edit = &deniedValue }}, {Annot, func(d *spec.Permissions) { d.Annot = &deniedValue }},
		{Signature, func(d *spec.Permissions) { d.Signature = &deniedValue }}, {Watermark, func(d *spec.Permissions) { d.Watermark = &deniedValue }},
		{PrintScreen, func(d *spec.Permissions) { d.PrintScreen = &deniedValue }},
	} {
		d := &spec.Permissions{}
		tc.deny(d)
		err := (Policy{}).Check(d, tc.action, 3)
		var denied *DeniedError
		if !errors.Is(err, ErrDenied) || !errors.As(err, &denied) || denied.Scope != 3 || denied.Action != tc.action || denied.Reason != Forbidden {
			t.Fatalf("拒绝丢失上下文：%v", err)
		}
		if err := (Policy{}).Check(d, Access, 3); err != nil {
			t.Fatalf("普通访问受其它操作限制：%v", err)
		}
	}
	d := &spec.Permissions{Edit: &deniedValue}
	for _, action := range []Action{Annot, Signature, Watermark} {
		if err := (Policy{}).Check(d, action, 0); err != nil {
			t.Fatalf("%s被错误地归入Edit：%v", action, err)
		}
	}
}

func TestPolicyPeriodBoundariesTimezoneAndMalformedDeclarations(t *testing.T) {
	start := time.Date(2026, 1, 1, 0, 0, 0, 500, time.UTC)
	end := start.Add(time.Hour)
	for _, tc := range []struct {
		name            string
		start, end, now time.Time
		reason          Reason
	}{
		{"尚未生效", start, end, start.Add(-time.Nanosecond), NotYetValid},
		{"包含开始时刻", start, end, start, ""},
		{"包含结束时刻", start, end, end, ""},
		{"结束后过期", start, end, end.Add(time.Nanosecond), Expired},
		{"仅结束时间", time.Time{}, end, end, ""},
		{"仅开始时间", start, time.Time{}, end, ""},
		{"同一瞬间不同时区", start, end, start.In(time.FixedZone("UTC+8", 8*3600)), ""},
		{"空期限", time.Time{}, time.Time{}, start, InvalidPeriod},
		{"倒置期限", end, start, start, InvalidPeriod},
	} {
		t.Run(tc.name, func(t *testing.T) {
			d := &spec.Permissions{ValidPeriod: &spec.ValidPeriod{StartDate: spec.DateTime{Time: tc.start}, EndDate: spec.DateTime{Time: tc.end}}}
			p := Policy{Now: func() time.Time { return tc.now }}
			for _, action := range []Action{Access, Copy, Export, Print, Edit, Annot, Signature, Watermark, PrintScreen} {
				err := p.Check(d, action, 0)
				if tc.reason == "" {
					if err != nil {
						t.Fatal(err)
					}
					continue
				}
				var denied *DeniedError
				if !errors.As(err, &denied) || denied.Reason != tc.reason {
					t.Fatalf("原因错误：%v", err)
				}
			}
		})
	}
}

func TestPolicyIgnoreRequiresExplicitModeAndStillValidatesActions(t *testing.T) {
	d := &spec.Permissions{ValidPeriod: &spec.ValidPeriod{}}
	if err := (Policy{Mode: IgnoreDeclarations}).Check(d, Access, 0); err != nil {
		t.Fatal(err)
	}
	if err := (Policy{Mode: IgnoreDeclarations}).Check(d, Action("unknown"), 0); !errors.Is(err, ErrAction) {
		t.Fatalf("忽略策略接受未知操作：%v", err)
	}
	if err := (Policy{Mode: Mode(99)}).Check(d, Access, 0); !errors.Is(err, ErrMode) {
		t.Fatalf("接受未知执行策略：%v", err)
	}
}

func TestPolicyReadsRawXMLDefaultsWithoutMutatingDeclarations(t *testing.T) {
	for _, source := range []string{`<Permissions/>`, `<Permissions><Print/></Permissions>`} {
		var d spec.Permissions
		if err := xml.Unmarshal([]byte(source), &d); err != nil {
			t.Fatal(err)
		}
		for _, action := range []Action{Access, Copy, Export, Print, Edit, Annot, Signature, Watermark, PrintScreen} {
			if err := (Policy{}).Check(&d, action, 0); err != nil {
				t.Fatal(err)
			}
		}
		if d.Edit != nil || d.Annot != nil || d.Export != nil || d.Signature != nil || d.Watermark != nil || d.PrintScreen != nil || d.ValidPeriod != nil {
			t.Fatalf("权限评估改写了原始声明：%+v", d)
		}
	}
	for _, source := range []string{
		`<Permissions><Print Printable="true" Copies="-5"/></Permissions>`,
		`<Permissions><Print Printable="true" Copies="2"/></Permissions>`,
	} {
		var d spec.Permissions
		if err := xml.Unmarshal([]byte(source), &d); err != nil {
			t.Fatal(err)
		}
		if err := (Policy{}).Check(&d, Print, 0); err != nil {
			t.Fatal(err)
		}
	}
}
