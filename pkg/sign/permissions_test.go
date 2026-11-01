package sign

import (
	"bytes"
	"context"
	"errors"
	"testing"
	"time"

	"github.com/zc310/ofd/pkg/creator"
	"github.com/zc310/ofd/pkg/creator/merge"
	"github.com/zc310/ofd/pkg/permission"
)

func TestSignPreflightsAllBodiesBeforeFirstSignerCall(t *testing.T) {
	denied := false
	inputs := make([][]byte, 0, 2)
	for _, value := range []*creator.Permissions{nil, {Signature: &denied}} {
		data, err := creator.Marshal(creator.Document{ID: "body", Pages: []creator.Page{{}}, Permissions: value})
		if err != nil {
			t.Fatal(err)
		}
		inputs = append(inputs, data)
	}
	var input, output bytes.Buffer
	if err := merge.Bytes(inputs, &input, merge.Options{}); err != nil {
		t.Fatal(err)
	}
	calls := 0
	err := Sign(t.Context(), input.Bytes(), &output, Options{Sign: func(context.Context, []byte) ([]byte, error) { calls++; return []byte("signed"), nil }})
	detail, ok := errors.AsType[*permission.DeniedError](err)
	if !ok || detail.Scope != 1 || calls != 0 || output.Len() != 0 {
		t.Fatalf("后续文档体拒绝前产生副作用：%v calls=%d", err, calls)
	}
}

func TestSignPermissionPreflightDoesNotInvokeSignerOrWrite(t *testing.T) {
	denied := false
	for _, value := range []*creator.Permissions{
		{Signature: &denied},
		{ValidPeriod: &creator.ValidPeriod{End: time.Date(2000, 1, 1, 0, 0, 0, 0, time.UTC)}},
	} {
		data, err := creator.Marshal(creator.Document{ID: "restricted", Pages: []creator.Page{{}}, Permissions: value})
		if err != nil {
			t.Fatal(err)
		}
		calls := 0
		options := Options{Sign: func(context.Context, []byte) ([]byte, error) { calls++; return []byte("signed"), nil }}
		var output bytes.Buffer
		err = Sign(t.Context(), data, &output, options)
		if !errors.Is(err, permission.ErrDenied) || calls != 0 || output.Len() != 0 {
			t.Fatalf("拒绝前产生签名副作用：%v calls=%d bytes=%d", err, calls, output.Len())
		}
		options.PermissionPolicy.Mode = permission.IgnoreDeclarations
		if err := Sign(t.Context(), data, &output, options); err != nil || calls != 1 {
			t.Fatalf("明确授权未生效：%v calls=%d", err, calls)
		}
	}
}

func TestSignIndependentOfEditAndRemoveRequiresEdit(t *testing.T) {
	denied := false
	data, err := creator.Marshal(creator.Document{ID: "sign-only", Pages: []creator.Page{{}}, Permissions: &creator.Permissions{Edit: &denied}})
	if err != nil {
		t.Fatal(err)
	}
	var signed bytes.Buffer
	if err := Sign(t.Context(), data, &signed, Options{Sign: func(context.Context, []byte) ([]byte, error) { return []byte("signed"), nil }}); err != nil {
		t.Fatalf("签名被Edit=false阻止：%v", err)
	}
	var output bytes.Buffer
	if err := Remove(t.Context(), signed.Bytes(), &output, RemoveOptions{}); !errors.Is(err, permission.ErrDenied) || output.Len() != 0 {
		t.Fatalf("移除签名未检查Edit：%v", err)
	}
}

func TestSignRechecksValidityAfterExternalSigner(t *testing.T) {
	end := time.Date(2026, 1, 2, 0, 0, 0, 0, time.UTC)
	now := end
	data, err := creator.Marshal(creator.Document{ID: "expires", Pages: []creator.Page{{}}, Permissions: &creator.Permissions{ValidPeriod: &creator.ValidPeriod{End: end}}})
	if err != nil {
		t.Fatal(err)
	}
	options := Options{PermissionPolicy: permission.Policy{Now: func() time.Time { return now }}, Sign: func(context.Context, []byte) ([]byte, error) {
		now = end.Add(time.Second)
		return []byte("signed"), nil
	}}
	var output bytes.Buffer
	err = Sign(t.Context(), data, &output, options)
	if !errors.Is(err, permission.ErrDenied) || output.Len() != 0 {
		t.Fatalf("签名期间到期后仍输出：%v", err)
	}
}

func TestRemoveUsesEditNotSignatureAndCleanupDoesNotBypassValidity(t *testing.T) {
	files := removalFixture()
	files["Work/Document.xml"] = `<Document xmlns="http://www.ofdspec.org/2016"><Permissions><Signature>false</Signature></Permissions></Document>`
	var output bytes.Buffer
	if err := Remove(t.Context(), removalZIP(t, files), &output, RemoveOptions{}); err != nil {
		t.Fatalf("签名移除错误地要求Signature：%v", err)
	}
	files["Work/Document.xml"] = `<Document xmlns="http://www.ofdspec.org/2016"><Permissions><ValidPeriod EndDate="2000-01-01T00:00:00Z"/></Permissions></Document>`
	output.Reset()
	err := Remove(t.Context(), removalZIP(t, files), &output, RemoveOptions{Cleanup: true})
	if !errors.Is(err, permission.ErrDenied) || output.Len() != 0 {
		t.Fatalf("Cleanup绕过有效期：%v", err)
	}
	if err := Remove(t.Context(), removalZIP(t, files), &output, RemoveOptions{Cleanup: true, PermissionPolicy: permission.Policy{Mode: permission.IgnoreDeclarations}}); err != nil {
		t.Fatal(err)
	}
}
