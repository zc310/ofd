package crypt

import (
	"bytes"
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"
)

func TestNormalizeOptionsDefaultsAndRejectsNegativeBudgets(t *testing.T) {
	options, err := NormalizeOptions(InspectOptions{})
	if err != nil || options.Mode != ModeStrict {
		t.Fatalf("零选项应使用 strict 模式: %+v, %v", options, err)
	}
	values := reflect.ValueOf(options.Limits)
	for i := 0; i < values.NumField(); i++ {
		name := values.Type().Field(i).Name
		if values.Field(i).Int() <= 0 {
			t.Errorf("缺省预算 %s 应为有限正值", name)
		}
		t.Run(name, func(t *testing.T) {
			var invalid InspectOptions
			reflect.ValueOf(&invalid.Limits).Elem().Field(i).SetInt(-1)
			if _, err := NormalizeOptions(invalid); err == nil {
				t.Fatal("负数预算应被拒绝")
			}
		})
	}
	if _, err := NormalizeOptions(InspectOptions{Mode: Mode("unknown")}); err == nil {
		t.Fatal("未知模式应被拒绝")
	}
	if got, err := NormalizeOptions(InspectOptions{Mode: ModeCompat, Limits: Limits{MaxEntries: 7}}); err != nil || got.Mode != ModeCompat || got.Limits.MaxEntries != 7 {
		t.Fatalf("显式模式和预算不应被覆盖: %+v, %v", got, err)
	}
}

func TestNormalizeEncryptOptionsRejectsAmbiguousSelectionsAndRecipients(t *testing.T) {
	for _, test := range []struct {
		name   string
		change func(*EncryptOptions)
		want   error
	}{
		{"no recipients", func(o *EncryptOptions) { o.Recipients = nil }, nil},
		{"empty recipient name", func(o *EncryptOptions) { o.Recipients[0].UserName = "" }, nil},
		{"invalid recipient name", func(o *EncryptOptions) { o.Recipients[0].UserName = "\xff" }, nil},
		{"invalid role", func(o *EncryptOptions) { o.Recipients[0].UserType = "Admin" }, nil},
		{"missing credentials", func(o *EncryptOptions) { o.Recipients[0].Password = nil }, nil},
		{"both credentials", func(o *EncryptOptions) { o.Recipients[0].Certificate = []byte{1} }, nil},
		{"invalid UTF-8 password", func(o *EncryptOptions) { o.Recipients[0].Password = []byte{0xff} }, nil},
		{"mixed schemes", func(o *EncryptOptions) {
			o.Recipients = append(o.Recipients, EncryptRecipient{UserName: "certificate", Certificate: []byte{1}})
		}, nil},
		{"recipient count", func(o *EncryptOptions) {
			o.Limits.MaxRecipients = 1
			o.Recipients = append(o.Recipients, o.Recipients[0])
		}, ErrLimitExceeded},
		{"recipient metadata", func(o *EncryptOptions) { o.Limits.MaxMetadataBytes = 1 }, ErrLimitExceeded},
		{"recipient metadata total", func(o *EncryptOptions) { o.Limits.MaxMetadataBytes = 257 }, ErrLimitExceeded},
		{"empty entries", func(o *EncryptOptions) { o.Entries = []string{} }, nil},
		{"duplicate entries", func(o *EncryptOptions) { o.Entries = []string{"OFD.xml", "OFD.xml"} }, nil},
		{"absolute entry", func(o *EncryptOptions) { o.Entries = []string{"/OFD.xml"} }, nil},
		{"traversal entry", func(o *EncryptOptions) { o.Entries = []string{"../OFD.xml"} }, nil},
		{"keep encrypted root", func(o *EncryptOptions) { o.KeepRoot = true; o.Entries = []string{"OFD.xml"} }, nil},
		{"pages with entries", func(o *EncryptOptions) { o.Pages = &PageSelection{}; o.Entries = []string{"OFD.xml"} }, nil},
		{"pages with append", func(o *EncryptOptions) { o.Pages = &PageSelection{}; o.Append = &DecryptOptions{} }, nil},
		{"noncanonical document path", func(o *EncryptOptions) { o.Pages = &PageSelection{Document: "/Doc_0/Document.xml", IDs: []string{"1"}} }, nil},
		{"missing page IDs", func(o *EncryptOptions) { o.Pages = &PageSelection{Document: "Doc_0/Document.xml"} }, nil},
		{"duplicate page IDs", func(o *EncryptOptions) {
			o.Pages = &PageSelection{Document: "Doc_0/Document.xml", IDs: []string{"1", "1"}}
		}, nil},
		{"noncanonical page ID", func(o *EncryptOptions) { o.Pages = &PageSelection{Document: "Doc_0/Document.xml", IDs: []string{"01"}} }, nil},
		{"zero page ID", func(o *EncryptOptions) { o.Pages = &PageSelection{Document: "Doc_0/Document.xml", IDs: []string{"0"}} }, nil},
		{"append encryptions entry", func(o *EncryptOptions) { o.Append = &DecryptOptions{}; o.Entries = []string{EntryPoint} }, nil},
		{"invalid append mode", func(o *EncryptOptions) { o.Append = &DecryptOptions{Mode: Mode("unknown")} }, nil},
	} {
		t.Run(test.name, func(t *testing.T) {
			options := testPasswordOptions()
			test.change(&options)
			if _, err := NormalizeEncryptOptions(options); err == nil || test.want != nil && !errors.Is(err, test.want) {
				t.Fatalf("无效选项应失败，实际 %v，期望 %v", err, test.want)
			}
		})
	}
	appendOptions := &DecryptOptions{Mode: ModeCompat, Password: []byte{}}
	options := testPasswordOptions()
	options.Append = appendOptions
	normalized, err := NormalizeEncryptOptions(options)
	if err != nil || normalized.Append == appendOptions || normalized.Append.Limits.MaxEntries == 0 || appendOptions.Limits.MaxEntries != 0 {
		t.Fatalf("规范化追加参数应使用独立副本: %+v, %v", normalized, err)
	}
	for _, invalid := range []DecryptOptions{{Password: []byte{0xff}}, {MissingScheme: PasswordScheme}, {Mode: ModeCompat, MissingScheme: "unknown"}} {
		if _, err := NormalizeDecryptOptions(invalid); err == nil {
			t.Fatal("无效解密选项应被拒绝")
		}
	}
	if got, err := NormalizeDecryptOptions(DecryptOptions{Mode: ModeCompat, MissingScheme: PasswordScheme, Password: []byte{}}); err != nil || got.Password == nil || got.MissingScheme != PasswordScheme {
		t.Fatalf("空口令与 compat 方案选择应保留: %+v, %v", got, err)
	}
}

func TestInspectCompatibilityModesAndResourceLimits(t *testing.T) {
	options := testPasswordOptions()
	options.PlaintextMap = true
	files, _ := encryptFiles(t, testPlainFiles(), options)
	for _, test := range []struct {
		name, code string
		change     func(map[string][]byte)
	}{
		{"entry point case", "encryption.entry_point_case", func(files map[string][]byte) { files["encryptions.xml"] = files[EntryPoint]; delete(files, EntryPoint) }},
		{"decimal operation ID", "encryption.id_nonconforming", func(files map[string][]byte) {
			for name, data := range files {
				if bytes.Contains(data, []byte(`ID="enc1"`)) {
					files[name] = bytes.ReplaceAll(data, []byte(`ID="enc1"`), []byte(`ID="1"`))
				}
			}
		}},
	} {
		t.Run(test.name, func(t *testing.T) {
			modified := make(map[string][]byte, len(files))
			for name, data := range files {
				modified[name] = bytes.Clone(data)
			}
			test.change(modified)
			pkg := testPackage(t, modified)
			if _, err := InspectPackage(t.Context(), pkg, InspectOptions{}); !errors.Is(err, ErrInvalidMetadata) {
				t.Fatalf("strict 应拒绝不规范元数据: %v", err)
			}
			info, err := InspectPackage(t.Context(), pkg, InspectOptions{Mode: ModeCompat})
			if err != nil {
				t.Fatal(err)
			}
			found := false
			for _, diagnostic := range info.Diagnostics {
				found = found || diagnostic.Code == test.code
			}
			if !found {
				t.Fatalf("compat 应记录具体兼容诊断 %q: %+v", test.code, info.Diagnostics)
			}
		})
	}
	pkg := testPackage(t, files)
	for _, limits := range []Limits{{MaxEntries: 1}, {MaxMetadataBytes: 1}, {MaxXMLNodes: 1}, {MaxXMLDepth: 1}} {
		if _, err := InspectPackage(t.Context(), pkg, InspectOptions{Limits: limits}); !errors.Is(err, ErrLimitExceeded) {
			t.Fatalf("检查预算应被执行: %+v, %v", limits, err)
		}
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err := InspectPackage(ctx, pkg, InspectOptions{}); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	plainInfo, err := InspectPackage(t.Context(), testPackage(t, testPlainFiles()), InspectOptions{})
	if err != nil || plainInfo.Encrypted || plainInfo.State != "none" {
		t.Fatalf("明文包不应被误报加密: %+v, %v", plainInfo, err)
	}
}

func TestEncryptBudgetsAndProtectedEntryFailBeforeOutput(t *testing.T) {
	for _, test := range []struct {
		name   string
		limits Limits
		entry  string
		want   error
	}{
		{"entry count", Limits{MaxEntries: 1}, "", ErrLimitExceeded},
		{"entry bytes", Limits{MaxEntryBytes: 1}, "", ErrLimitExceeded},
		{"total bytes", Limits{MaxTotalBytes: 1}, "", ErrLimitExceeded},
		{"mapping count", Limits{MaxMappings: 1}, "", ErrLimitExceeded},
		{"missing selected entry", Limits{}, "missing.xml", ErrInvalidMetadata},
		{"integrity manifest", Limits{}, "OFDEntries.xml", ErrProtectedContent},
	} {
		t.Run(test.name, func(t *testing.T) {
			files := testPlainFiles()
			options := testPasswordOptions()
			options.Limits = test.limits
			if test.entry == "OFDEntries.xml" {
				files[test.entry] = []byte("protected manifest")
			} else if test.entry != "" {
				options.Entries = []string{test.entry}
			}
			called := false
			report, err := EncryptPackage(t.Context(), testPackage(t, files), options, func(string, []byte) error { called = true; return nil })
			if !errors.Is(err, test.want) || report != nil || called {
				t.Fatalf("预检失败不应输出: report=%+v, err=%v, emitted=%t", report, err, called)
			}
		})
	}
}

func TestLockedErrorPreservesCauseAndMissingCredentialClassification(t *testing.T) {
	locked := &LockedError{Cause: ErrInvalidMetadata}
	if !errors.Is(locked, ErrLocked) || !errors.Is(locked, ErrInvalidMetadata) || !strings.Contains(locked.Error(), ErrInvalidMetadata.Error()) {
		t.Fatalf("锁定错误应保留分类与原因: %v", locked)
	}
	if !missingCredentials(ErrCredentialsRequired) {
		t.Fatal("缺失凭据应允许部分视图")
	}
	for _, cause := range []error{context.Canceled, context.DeadlineExceeded, ErrDecryptFailed, ErrLimitExceeded, ErrUnsupported, ErrInvalidMetadata, ErrProtectedContent} {
		if missingCredentials(errors.Join(ErrCredentialsRequired, cause)) {
			t.Errorf("缺失凭据不能遮蔽其他失败: %v", cause)
		}
	}
}
