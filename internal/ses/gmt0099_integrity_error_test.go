package ses_test

import (
	"bytes"
	"context"
	"errors"
	"testing"
	"time"

	"github.com/zc310/ofd/pkg/crypt"
	"github.com/zc310/ofd/pkg/integrity"
)

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
