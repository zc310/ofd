package workflow

import (
	"archive/zip"
	"bytes"
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"errors"
	"io"
	"math/big"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/emmansun/gmsm/smx509"
	"github.com/zc310/ofd/internal/container"
	protocol "github.com/zc310/ofd/internal/crypt"
	"github.com/zc310/ofd/pkg/creator"
	"github.com/zc310/ofd/pkg/creator/replace"
	"github.com/zc310/ofd/pkg/creator/watermark"
	"github.com/zc310/ofd/pkg/permission"
	"github.com/zc310/ofd/pkg/sign"
	"github.com/zc310/ofd/pkg/spec"
)

type testEvidence struct {
	at                   time.Time
	original, copyDER    []byte
	originalKey, copyKey *ecdsa.PrivateKey
	verification         sign.VerificationOptions
}

func evidence(t *testing.T) testEvidence {
	t.Helper()
	at := time.Date(2026, 11, 1, 10, 0, 0, 0, time.UTC)
	newKey := func() *ecdsa.PrivateKey {
		key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
		if err != nil {
			t.Fatal(err)
		}
		return key
	}
	caKey := newKey()
	caTemplate := &x509.Certificate{SerialNumber: big.NewInt(1), Subject: pkix.Name{CommonName: "临时证照测试CA"},
		NotBefore: at.Add(-24 * time.Hour), NotAfter: at.Add(365 * 24 * time.Hour), IsCA: true, BasicConstraintsValid: true,
		KeyUsage: x509.KeyUsageCertSign | x509.KeyUsageCRLSign, SubjectKeyId: []byte{1, 2, 3}}
	caDER, err := x509.CreateCertificate(rand.Reader, caTemplate, caTemplate, &caKey.PublicKey, caKey)
	if err != nil {
		t.Fatal(err)
	}
	ca, err := x509.ParseCertificate(caDER)
	if err != nil {
		t.Fatal(err)
	}
	leaf := func(serial int64, key *ecdsa.PrivateKey) []byte {
		template := &x509.Certificate{SerialNumber: big.NewInt(serial), Subject: pkix.Name{CommonName: "不能仅凭名称授权"},
			NotBefore: at.Add(-time.Hour), NotAfter: at.Add(24 * time.Hour), KeyUsage: x509.KeyUsageDigitalSignature}
		der, err := x509.CreateCertificate(rand.Reader, template, ca, &key.PublicKey, caKey)
		if err != nil {
			t.Fatal(err)
		}
		return der
	}
	originalKey, copyKey := newKey(), newKey()
	root, err := smx509.ParseCertificate(caDER)
	if err != nil {
		t.Fatal(err)
	}
	roots := smx509.NewCertPool()
	roots.AddCert(root)
	crl, err := x509.CreateRevocationList(rand.Reader, &x509.RevocationList{
		Number: big.NewInt(1), ThisUpdate: at.Add(-time.Hour), NextUpdate: at.Add(24 * time.Hour),
	}, ca, caKey)
	if err != nil {
		t.Fatal(err)
	}
	return testEvidence{at: at, original: leaf(2, originalKey), copyDER: leaf(3, copyKey),
		originalKey: originalKey, copyKey: copyKey,
		verification: sign.VerificationOptions{CurrentTime: at.Add(time.Hour),
			Trust:      &sign.CertificateTrustOptions{Roots: roots},
			Revocation: &sign.CertificateRevocationOptions{CRLs: [][]byte{crl}, Issuers: [][]byte{caDER}}}}
}

func (e testEvidence) authorize(_ context.Context, request Authorization) error {
	want := e.original
	if request.Role == Copy {
		want = e.copyDER
		if creators := request.Metadata.Values("certificateCopyCreator"); len(creators) != 1 || creators[0] != "示例制作方" {
			return errors.New("制作方元数据未绑定")
		}
	}
	if !bytes.Equal(want, request.SignerCertificate) {
		return errors.New("签名者并非当前角色获准的证书")
	}
	return nil
}

func (e testEvidence) signing(role Role, id string) LockOptions {
	der, key := e.original, e.originalKey
	if role == Copy {
		der, key = e.copyDER, e.copyKey
	}
	return LockOptions{Verification: e.verification, Authorize: e.authorize, Sign: sign.Options{
		ID: id, ProviderName: "workflow-test", SignatureMethod: sign.ECDSAWithSHA256, CheckMethod: "SHA256",
		Date: e.at, Deterministic: true,
		Sign: func(_ context.Context, message []byte) ([]byte, error) {
			return sign.CreateDigitalSignedValue(message, der, key, sign.DigitalSigningOptions{Time: e.at, Algorithm: sign.ECDSAWithSHA256})
		},
	}}
}

func unsigned(t *testing.T) []byte {
	t.Helper()
	return unsignedWithPermissions(t, nil)
}

func unsignedWithPermissions(t *testing.T, permissions *creator.Permissions) []byte {
	t.Helper()
	doc := creator.Document{ID: "certificate-workflow", Pages: []creator.Page{{}, {}}, Keywords: []string{"测试", "工作流"},
		Permissions: permissions,
		CustomDatas: []creator.CustomData{
			{Name: "certificateName", Value: "测试证照"}, {Name: "ZZLXDM", Value: "11100000000013338W002"},
			{Name: "ZZBH", Value: "测试-001"}, {Name: "certificateIdentifier", Value: "1.2.156.3005.2.11100000000013338W002.11320482014138860P.201807250201.001.M"},
			{Name: "ZZBFJG", Value: "示例颁发机构"}, {Name: "ZZBFRQ", Value: "2018年7月25日"}, {Name: "CZZT", Value: "示例主体"},
		},
		CustomTags: []creator.CustomTag{{TypeID: "cert", Data: []byte(`<Certificate/>`)}},
	}
	data, err := creator.MarshalWithOptions(doc, creator.CreateOptions{Deterministic: true})
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func original(t *testing.T, e testEvidence, business bool, rootCovered bool) []byte {
	t.Helper()
	data := unsigned(t)
	if business {
		o := e.signing(Original, "business").Sign
		o.Type, o.Append = spec.SigTypeSign, true
		o.References = &sign.ReferenceOptions{Include: []string{"Pages/**"}, RootDocument: rootCovered}
		var output bytes.Buffer
		if err := sign.Sign(t.Context(), data, &output, o); err != nil {
			t.Fatal(err)
		}
		data = output.Bytes()
	}
	var result bytes.Buffer
	if err := SignOriginal(t.Context(), data, &result, e.signing(Original, "original-lock")); err != nil {
		t.Fatal(err)
	}
	return result.Bytes()
}

func (e testEvidence) copyOptions() CopyOptions {
	return CopyOptions{
		Source:   VerifyOptions{LockID: "original-lock", Role: Original, Verification: e.verification, Authorize: e.authorize},
		Lock:     e.signing(Copy, "copy-lock"),
		Metadata: CopyMetadata{CreationTime: e.at, Creator: "示例制作方", Cause: "仅供本次测试事项使用", ExpiringDate: "2026-11-02"},
		Edit:     addWatermark,
	}
}

func addWatermark(ctx context.Context, input []byte) ([]byte, error) {
	return watermarkWithPolicy(ctx, input, permission.Policy{})
}

func watermarkWithPolicy(ctx context.Context, input []byte, policy permission.Policy) ([]byte, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	var result bytes.Buffer
	//nolint:modernize // 保持已提交 go.mod 的 Go 1.26 语法，不依赖本地 Go 1.27 的嵌入字段字面量简写。
	options := watermark.Options{Options: replace.Options{Signatures: creator.SignaturePreserve, Deterministic: true, PermissionPolicy: policy}}
	err := watermark.Add(input, watermark.Target{}, watermark.Watermark{
		Creator: "示例制作方", LastModDate: time.Date(2026, 11, 1, 0, 0, 0, 0, time.UTC), Remark: "仅供本次测试事项使用",
		Boundary:   &creator.Box{X: 10, Y: 10, Width: 20, Height: 10},
		Appearance: []byte(`<PathObject ID="900" Boundary="0 0 20 10" Fill="true" Stroke="false"><FillColor Value="255 0 0"/><AbbreviatedData>M 0 0 L 20 0 L 20 10 L 0 10 C</AbbreviatedData></PathObject>`),
	}, &result, options)
	return result.Bytes(), err
}

func TestOriginalLockAndCopyPreserveBusinessSignaturesAndOriginalBytes(t *testing.T) {
	e := evidence(t)
	input := original(t, e, true, false)
	source := bytes.Clone(input)
	if err := VerifyLock(t.Context(), input, e.copyOptions().Source); err != nil {
		t.Fatal(err)
	}
	var output bytes.Buffer
	if err := CreateCopy(t.Context(), input, &output, e.copyOptions()); err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(source, input) {
		t.Fatal("源原件被修改")
	}
	if err := VerifyLock(t.Context(), output.Bytes(), VerifyOptions{
		LockID: "copy-lock", Role: Copy, Verification: e.verification, Authorize: e.authorize,
	}); err != nil {
		t.Fatal(err)
	}
	if err := VerifyLock(t.Context(), output.Bytes(), VerifyOptions{
		LockID: "copy-lock", Role: Original, Verification: e.verification, Authorize: e.authorize,
	}); !errors.Is(err, ErrInvalid) {
		t.Fatalf("加注件不能仅改用途参数伪装为原件: %v", err)
	}
	before, err := inspect(t.Context(), input)
	if err != nil {
		t.Fatal(err)
	}
	defer before.close()
	after, err := inspect(t.Context(), output.Bytes())
	if err != nil {
		t.Fatal(err)
	}
	defer after.close()
	old, err := before.selected("business")
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range old.artifacts {
		a, err := before.pkg.Read(name)
		if err != nil {
			t.Fatal(err)
		}
		b, err := after.pkg.Read(name)
		if err != nil || !bytes.Equal(a, b) {
			t.Fatalf("业务签名原字节未保留: %s, %v", name, err)
		}
	}
	if _, err := after.selected("original-lock"); !errors.Is(err, ErrInvalid) {
		t.Fatal("旧锁定签名未移除")
	}
	if len(after.signatures) != 2 || len(after.metadata.Values("certificateCopyCause")) != 1 {
		t.Fatalf("加注件签名或元数据不完整: %+v", after.signatures)
	}
}

func TestLockRequiresFullEvidenceAndExplicitRoleAuthorization(t *testing.T) {
	e := evidence(t)
	input := original(t, e, false, false)
	check := e.copyOptions().Source
	check.Verification = sign.VerificationOptions{CurrentTime: e.at.Add(time.Hour)}
	called := false
	check.Authorize = func(context.Context, Authorization) error { called = true; return nil }
	if err := VerifyLock(t.Context(), input, check); !errors.Is(err, ErrEvidenceRequired) || called {
		t.Fatalf("缺少信任吊销证据不得交给授权回调或报告通过: %v, called=%t", err, called)
	}
	results, err := sign.Verify(t.Context(), input, check.Verification)
	if err != nil || len(results) != 1 || !results[0].Result.Valid || results[0].Result.FullyValid() {
		t.Fatalf("测试应覆盖数学通过但完整证据缺失: %+v %v", results, err)
	}
	check = e.copyOptions().Source
	check.Authorize = nil
	if err := VerifyLock(t.Context(), input, check); !errors.Is(err, ErrEvidenceRequired) {
		t.Fatal(err)
	}
	denied := errors.New("外部登记未授权")
	check.Authorize = func(context.Context, Authorization) error { return denied }
	if err := VerifyLock(t.Context(), input, check); !errors.Is(err, ErrAuthorization) || !errors.Is(err, denied) {
		t.Fatalf("授权拒绝应保留错误链: %v", err)
	}
	// 同一CA签发、Subject相同，也不能代替实际签名者的角色授权。
	wrong := e.signing(Copy, "wrong-role")
	var output bytes.Buffer
	if err := SignOriginal(t.Context(), unsigned(t), &output, wrong); !errors.Is(err, ErrAuthorization) || output.Len() != 0 {
		t.Fatalf("非颁发方不能作为原件被验收: %v", err)
	}
}

func TestCopyRefusesToInvalidateOrDropBusinessSignatures(t *testing.T) {
	e := evidence(t)
	for _, scenario := range []string{"root-protected", "drop-business", "modify-business", "noop"} {
		t.Run(scenario, func(t *testing.T) {
			input := original(t, e, true, scenario == "root-protected")
			o := e.copyOptions()
			editorCalled, signerCalled := false, false
			realSign := o.Lock.Sign.Sign
			o.Lock.Sign.Sign = func(ctx context.Context, data []byte) ([]byte, error) {
				signerCalled = true
				return realSign(ctx, data)
			}
			o.Edit = func(ctx context.Context, data []byte) ([]byte, error) {
				editorCalled = true
				switch scenario {
				case "drop-business":
					var out bytes.Buffer
					err := sign.Remove(ctx, data, &out, sign.RemoveOptions{})
					return out.Bytes(), err
				case "modify-business":
					return rewrite(t, data, func(files map[string][]byte) {
						for name := range files {
							if strings.HasSuffix(name, "business.dat") {
								files[name] = []byte("tampered")
							}
						}
					}), nil
				case "noop":
					return data, nil
				default:
					return addWatermark(ctx, data)
				}
			}
			var output bytes.Buffer
			err := CreateCopy(t.Context(), input, &output, o)
			if err == nil || output.Len() != 0 || signerCalled {
				t.Fatalf("不安全修改不应重签或输出: %v", err)
			}
			if scenario == "root-protected" && editorCalled {
				t.Fatal("元数据已影响业务签名时不能继续编辑")
			}
		})
	}
}

func TestLockCoverageRejectsUncoveredFilesAndTamperedList(t *testing.T) {
	e := evidence(t)
	input := original(t, e, false, false)
	for _, scenario := range []string{"extra-file", "tampered-list", "tampered-page"} {
		t.Run(scenario, func(t *testing.T) {
			data := rewrite(t, input, func(files map[string][]byte) {
				switch scenario {
				case "extra-file":
					files["Doc_0/unsigned.bin"] = []byte("unsigned")
				case "tampered-list":
					files["Doc_0/Signatures.xml"] = bytes.ReplaceAll(files["Doc_0/Signatures.xml"], []byte("original-lock"), []byte("changed-lock"))
				case "tampered-page":
					for name, raw := range files {
						if strings.Contains(name, "Pages/") && strings.HasSuffix(name, ".xml") {
							files[name] = append(raw, []byte("<!-- tampered -->")...)
						}
					}
				}
			})
			if err := VerifyLock(t.Context(), data, e.copyOptions().Source); err == nil {
				t.Fatal("篡改或漏签不应通过")
			}
		})
	}
}

func TestLockedOriginalCannotBeExtendedAndInvalidOptionsDoNotCallSigner(t *testing.T) {
	e := evidence(t)
	input := original(t, e, false, false)
	o := e.signing(Original, "next-lock")
	called := false
	o.Sign.Sign = func(context.Context, []byte) ([]byte, error) { called = true; return nil, errors.New("不应调用") }
	var output bytes.Buffer
	if err := SignOriginal(t.Context(), input, &output, o); !errors.Is(err, protocol.ErrProtectedContent) || called || output.Len() != 0 {
		t.Fatalf("锁定列表不应继续追加: %v", err)
	}
	o.Sign.References = &sign.ReferenceOptions{}
	if err := SignOriginal(t.Context(), unsigned(t), &output, o); !errors.Is(err, ErrOptions) || called {
		t.Fatal(err)
	}
}

func TestWorkflowCancellationAndShortWrites(t *testing.T) {
	e := evidence(t)
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	var output bytes.Buffer
	if err := SignOriginal(ctx, nil, &output, e.signing(Original, "lock")); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	if err := CreateCopy(ctx, nil, &output, e.copyOptions()); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	if err := VerifyLock(ctx, nil, VerifyOptions{}); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	o := e.signing(Original, "cancel-lock")
	ctx, cancel = context.WithCancel(t.Context())
	o.Sign.Sign = func(context.Context, []byte) ([]byte, error) { cancel(); return nil, ctx.Err() }
	if err := SignOriginal(ctx, unsigned(t), &output, o); !errors.Is(err, context.Canceled) || output.Len() != 0 {
		t.Fatal(err)
	}
	if err := SignOriginal(t.Context(), unsigned(t), shortWriter{}, e.signing(Original, "short-lock")); !errors.Is(err, io.ErrShortWrite) {
		t.Fatal(err)
	}
}

type shortWriter struct{}

func (shortWriter) Write(data []byte) (int, error) { return len(data) - 1, nil }

func rewrite(t *testing.T, input []byte, change func(map[string][]byte)) []byte {
	t.Helper()
	pkg, err := container.OpenBytes(input)
	if err != nil {
		t.Fatal(err)
	}
	defer pkg.Close()
	files := map[string][]byte{}
	for _, entry := range pkg.Entries() {
		if !entry.IsDir {
			raw, err := pkg.Read(entry.Path)
			if err != nil {
				t.Fatal(err)
			}
			files[entry.Path] = raw
		}
	}
	change(files)
	var output bytes.Buffer
	writer := zip.NewWriter(&output)
	names := make([]string, 0, len(files))
	for name := range files {
		names = append(names, name)
	}
	slices.Sort(names)
	for _, name := range names {
		w, err := writer.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := w.Write(files[name]); err != nil {
			t.Fatal(err)
		}
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	return output.Bytes()
}

func TestCopyMetadataRequiresAllFieldsAndValidPeriod(t *testing.T) {
	for _, m := range []CopyMetadata{
		{}, {CreationTime: time.Now(), Creator: "制作方", Cause: "事由", ExpiringDate: "长期"},
		{CreationTime: time.Date(2026, 11, 1, 0, 0, 0, 0, time.UTC), Creator: "制作方", Cause: "事由", ExpiringDate: "2026-10-31"},
	} {
		if err := m.validate(); !errors.Is(err, ErrOptions) {
			t.Fatalf("%+v: %v", m, err)
		}
	}
	if copyField("JZJZZSJ") != "certificateCopyCreationTime" {
		t.Fatal("短名未识别")
	}
}

func TestCopyUsesSourcePermissionsBeforeEditor(t *testing.T) {
	e := evidence(t)
	for _, scenario := range []string{"export", "edit"} {
		t.Run(scenario, func(t *testing.T) {
			deny := false
			permissions := &creator.Permissions{}
			if scenario == "export" {
				permissions.Export = &deny
			}
			if scenario == "edit" {
				permissions.Edit = &deny
			}
			var original bytes.Buffer
			if err := SignOriginal(t.Context(), unsignedWithPermissions(t, permissions), &original, e.signing(Original, "original-lock")); err != nil {
				t.Fatal(err)
			}
			options := e.copyOptions()
			called := false
			options.Edit = func(ctx context.Context, data []byte) ([]byte, error) {
				called = true
				return addWatermark(ctx, data)
			}
			var output bytes.Buffer
			if err := CreateCopy(t.Context(), original.Bytes(), &output, options); !errors.Is(err, permission.ErrDenied) || called || output.Len() != 0 {
				t.Fatalf("拒绝源权限时不得调用编辑器或输出: %v", err)
			}
		})
	}
}

func TestCopyRechecksSourceValidityAfterFinalAuthorization(t *testing.T) {
	e := evidence(t)
	now := e.at
	policy := permission.Policy{Now: func() time.Time { return now }}
	options := e.copyOptions()
	options.Lock.Sign.PermissionPolicy = policy
	options.Edit = func(ctx context.Context, data []byte) ([]byte, error) {
		return watermarkWithPolicy(ctx, data, policy)
	}
	options.Lock.Authorize = func(ctx context.Context, request Authorization) error {
		if err := e.authorize(ctx, request); err != nil {
			return err
		}
		now = e.at.Add(48 * time.Hour)
		return nil
	}
	permissions := &creator.Permissions{ValidPeriod: &creator.ValidPeriod{
		Start: e.at.Add(-time.Hour), End: e.at.Add(24 * time.Hour),
	}}
	originalOptions := e.signing(Original, "original-lock")
	originalOptions.Sign.PermissionPolicy = policy
	var original, output bytes.Buffer
	if err := SignOriginal(t.Context(), unsignedWithPermissions(t, permissions), &original, originalOptions); err != nil {
		t.Fatal(err)
	}
	if err := CreateCopy(t.Context(), original.Bytes(), &output, options); !errors.Is(err, permission.ErrDenied) || output.Len() != 0 {
		t.Fatalf("最终授权后源声明失效也不得发布: %v", err)
	}
}
