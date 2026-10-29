package ses_test

import (
	"bytes"
	"context"
	"errors"
	"io"
	"testing"
	"time"

	"github.com/zc310/ofd/pkg/crypt"
	"github.com/zc310/ofd/pkg/integrity"
)

// 新增清单和签名值也必须计入输出预算，且保留公开的超限错误分类。
func TestGMT0099IntegrityProtectBudgetsIncludeManifestAndSignedValue(t *testing.T) {
	f := newSigningFixture(t, false)
	f.options.CurrentTime = f.now.Add(2 * time.Second)
	files := gmtFiles()
	input := gmtArchive(t, files)
	options := integrity.ProtectOptions{
		Date:    f.now,
		Options: integrity.Options{Verification: *f.options},
	}
	signer := digitalSigner(t, f, true)
	var manifest, value []byte
	options.Sign = func(ctx context.Context, data []byte) ([]byte, error) {
		if value == nil {
			manifest = bytes.Clone(data)
			var err error
			value, err = signer(ctx, data)
			return value, err
		}
		if !bytes.Equal(manifest, data) {
			t.Fatal("预算设置改变了待签清单字节")
		}
		return bytes.Clone(value), nil
	}
	var output bytes.Buffer
	if _, err := integrity.Protect(t.Context(), input, &output, options); err != nil {
		t.Fatal(err)
	}
	protected := gmtUnpack(t, output.Bytes())
	var total, largest int64
	for name, content := range protected {
		total += int64(len(content))
		largest = max(largest, int64(len(content)))
		if original, exists := files[name]; exists && !bytes.Equal(original, content) {
			t.Fatalf("原文件字节发生变化: %s", name)
		}
	}
	for _, test := range []struct {
		name   string
		limits crypt.Limits
	}{
		{"entries", crypt.Limits{MaxEntries: len(protected) - 1}},
		{"entry_bytes", crypt.Limits{MaxEntryBytes: largest - 1}},
		{"total_bytes", crypt.Limits{MaxTotalBytes: total - 1}},
	} {
		t.Run(test.name, func(t *testing.T) {
			opt := options
			opt.Limits = test.limits
			report, err := integrity.Protect(t.Context(), input, io.Discard, opt)
			if !errors.Is(err, crypt.ErrLimitExceeded) || report != nil {
				t.Fatalf("输出预算未生效或错误分类改变: report=%+v err=%v", report, err)
			}
		})
	}
	options.Limits = crypt.Limits{MaxEntries: len(protected), MaxEntryBytes: largest, MaxTotalBytes: total}
	if _, err := integrity.Protect(t.Context(), input, io.Discard, options); err != nil {
		t.Fatalf("恰好满足输出预算时应成功: %v", err)
	}
}

func TestGMT0099IntegrityErrorsNeverReturnValidPartialReport(t *testing.T) {
	f := newSigningFixture(t, false)
	f.options.CurrentTime = f.now.Add(2 * time.Second)
	protected := protectGMT(t, f, physicalSigned(t, f, gmtFiles(), false), true)
	for _, name := range []string{"wrong_password", "credential_budget", "processed_budget", "metadata_budget", "canceled_resolver", "invalid_manifest", "previous_failure"} {
		t.Run(name, func(t *testing.T) {
			ctx := t.Context()
			input := protected
			options := integrity.Options{Verification: *f.options, Decrypt: &crypt.DecryptOptions{Password: []byte("secret")}}
			var expected error
			status := "incomplete"
			switch name {
			case "wrong_password":
				options.Decrypt.Password = []byte("wrong")
				expected = crypt.ErrDecryptFailed
			case "credential_budget":
				options.Limits.MaxCredentialAttempts = 1
				expected = crypt.ErrLimitExceeded
			case "processed_budget":
				options.Limits.MaxProcessedBytes = 32
				expected = crypt.ErrLimitExceeded
			case "metadata_budget":
				// 清单和签名各自可读，但累计预算不足以继续发现物理签名。
				files := gmtUnpack(t, input)
				inspection, err := integrity.Inspect(t.Context(), input, integrity.Options{})
				if err != nil {
					t.Fatal(err)
				}
				options.Limits.MaxTotalMetadataBytes = int64(len(files[integrity.EntryPoint]) + len(files[inspection.Manifest.SignedValueLoc]) + 16)
				expected = crypt.ErrLimitExceeded
			case "canceled_resolver":
				var cancel context.CancelFunc
				ctx, cancel = context.WithCancel(t.Context())
				defer cancel()
				options.Decrypt = &crypt.DecryptOptions{Resolve: func(context.Context, crypt.CredentialRequest) (crypt.Credentials, error) {
					cancel()
					return crypt.Credentials{}, ctx.Err()
				}}
				expected = context.Canceled
			case "invalid_manifest":
				files := gmtUnpack(t, input)
				files[integrity.EntryPoint] = bytes.Replace(files[integrity.EntryPoint], []byte("FileList"), []byte("Unknown"), 1)
				input = gmtArchive(t, files)
				expected, status = integrity.ErrInvalid, "invalid"
			case "previous_failure":
				files := gmtUnpack(t, input)
				files["unlisted.bin"] = []byte("unlisted")
				input = gmtArchive(t, files)
				options.Decrypt.Password = []byte("wrong")
				expected, status = crypt.ErrDecryptFailed, "invalid"
			}
			report, err := integrity.Verify(ctx, input, options)
			if !errors.Is(err, expected) || report == nil || report.Status != status {
				t.Fatalf("status=%s expected error=%v report=%+v err=%v", status, expected, report, err)
			}
			found := false
			for _, check := range report.Checks {
				if check.Code == "verification" && check.Status != "valid" {
					found = true
				}
			}
			if !found {
				t.Fatalf("未保留验证中止原因: %+v", report)
			}
		})
	}
}
