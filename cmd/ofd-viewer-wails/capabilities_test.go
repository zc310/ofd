package main

import (
	"archive/zip"
	"bytes"
	"context"
	"crypto/rand"
	"crypto/x509/pkix"
	"encoding/pem"
	"errors"
	"image/png"
	"math/big"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/emmansun/gmsm/pkcs8"
	"github.com/emmansun/gmsm/sm2"
	"github.com/emmansun/gmsm/smx509"
	readersession "github.com/zc310/ofd/internal/reader/session"
	"github.com/zc310/ofd/pkg/crypt"
	"github.com/zc310/ofd/pkg/reader"
)

// 同一个最小包保护打印权限、独立历史入口及解密后版本切换。
func capabilityPackage(t *testing.T, permissions string) []byte {
	t.Helper()
	var output bytes.Buffer
	archive := zip.NewWriter(&output)
	for path, content := range map[string]string{
		"OFD.xml":                     `<OFD xmlns="http://www.ofdspec.org/2016" Version="1.0" DocType="OFD"><DocBody><DocInfo><DocID>capabilities</DocID></DocInfo><DocRoot>Main/Document.xml</DocRoot><Versions><Version ID="v1" Index="1" BaseLoc="History/Version.xml"/><Version ID="denied" Index="2" BaseLoc="History/Denied.xml"/></Versions></DocBody></OFD>`,
		"Main/Document.xml":           `<Document><CommonData><PageArea><PhysicalBox>0 0 30 20</PhysicalBox></PageArea></CommonData><Pages><Page ID="1" BaseLoc="Page.xml"/></Pages>` + permissions + `</Document>`,
		"Main/Page.xml":               `<Page><Content><Layer ID="2"/></Content></Page>`,
		"History/Version.xml":         `<DocVersion ID="v1" Name="历史"><FileList><File ID="1">Old/Document.xml</File><File ID="2">Old/Page.xml</File></FileList><DocRoot>Old/Document.xml</DocRoot></DocVersion>`,
		"History/Denied.xml":          `<DocVersion ID="denied"><FileList><File ID="1">Denied/Document.xml</File><File ID="2">Old/Page.xml</File></FileList><DocRoot>Denied/Document.xml</DocRoot></DocVersion>`,
		"History/Old/Document.xml":    `<Document><CommonData><PageArea><PhysicalBox>0 0 60 40</PhysicalBox></PageArea></CommonData><Pages><Page ID="1" BaseLoc="Page.xml"/></Pages><Permissions><Export>false</Export></Permissions></Document>`,
		"History/Old/Page.xml":        `<Page><Content><Layer ID="2"/></Content></Page>`,
		"History/Denied/Document.xml": `<Document><CommonData><PageArea><PhysicalBox>0 0 60 40</PhysicalBox></PageArea></CommonData><Pages><Page ID="1" BaseLoc="../Old/Page.xml"/></Pages><Permissions><ValidPeriod><EndDate>2000-01-01T00:00:00</EndDate></ValidPeriod></Permissions></Document>`,
	} {
		entry, err := archive.Create(path)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := entry.Write([]byte(content)); err != nil {
			t.Fatal(err)
		}
	}
	if err := archive.Close(); err != nil {
		t.Fatal(err)
	}
	return output.Bytes()
}

func capabilityDesktop(t *testing.T, data []byte) (*Desktop, *openedDocument) {
	t.Helper()
	input := filepath.Join(t.TempDir(), "input.ofd")
	if err := os.WriteFile(input, data, 0o600); err != nil {
		t.Fatal(err)
	}
	d := &Desktop{document: &readersession.Service{}}
	t.Cleanup(d.shutdown)
	opened, err := d.openPath(input)
	if err != nil {
		t.Fatal(err)
	}
	return d, opened
}

func TestNativeVersionsRotateResourcesRestoreMainAndKeepViewOnFailure(t *testing.T) {
	d, opened := capabilityDesktop(t, capabilityPackage(t, ""))
	versions, err := d.Versions(t.Context(), opened.PagePrefix)
	if err != nil || len(versions) != 2 {
		t.Fatalf("版本清单 = %v, %v", versions, err)
	}
	next, err := d.SelectVersion(opened.PagePrefix, 0, "v1")
	if err != nil || len(next.Pages) != 1 || next.Pages[0].Width != 60 || next.PagePrefix == opened.PagePrefix {
		t.Fatalf("独立历史版本 = %+v, %v", next, err)
	}
	if _, err := d.PageInfo(opened.PagePrefix, 0); !errors.Is(err, readersession.ErrStaleDocument) {
		t.Fatalf("旧路径仍有效: %v", err)
	}
	if err := d.document.CheckPermission(next.PagePrefix, reader.PermissionExport, []int{0}); !errors.Is(err, reader.ErrPermissionDenied) {
		t.Fatalf("历史权限 = %v", err)
	}
	for _, id := range []string{"missing", "denied"} {
		if _, err := d.SelectVersion(next.PagePrefix, 0, id); err == nil {
			t.Fatalf("无效版本 %s 被发布", id)
		}
		if page, err := d.PageInfo(next.PagePrefix, 0); err != nil || page.Width != 60 {
			t.Fatalf("失败覆盖当前版本: %v, %v", page, err)
		}
	}
	main, err := d.SelectVersion(next.PagePrefix, 0, "")
	if err != nil || main.Pages[0].Width != 30 {
		t.Fatalf("恢复主入口 = %+v, %v", main, err)
	}
	if tags, err := d.CustomTags(t.Context(), main.PagePrefix); err != nil || tags == nil || len(tags) != 0 {
		t.Fatalf("空标签契约 = %v, %v", tags, err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err := d.CustomTags(ctx, main.PagePrefix); !errors.Is(err, context.Canceled) {
		t.Fatalf("标签取消 = %v", err)
	}
}

func TestNativePasswordUnlockRetriesCancelsAndSupportsVersionSwitch(t *testing.T) {
	for _, password := range []string{"", "  中文\ufeff\n  "} {
		t.Run(password, func(t *testing.T) {
			var encrypted bytes.Buffer
			if _, err := crypt.Encrypt(t.Context(), capabilityPackage(t, ""), &encrypted, crypt.EncryptOptions{Recipients: []crypt.EncryptRecipient{{UserName: "User", Password: []byte(password)}}}); err != nil {
				t.Fatal(err)
			}
			d, challenge := capabilityDesktop(t, encrypted.Bytes())
			if challenge.Code != "credentials_required" || challenge.Challenge == "" || challenge.PagePrefix != "" {
				t.Fatalf("解锁挑战 = %+v", challenge)
			}
			pick := func(string) (string, error) { t.Fatal("直接口令不能选择文件"); return "", nil }
			wrong := "wrong"
			retry, err := d.unlockWithPicker(t.Context(), challenge.Challenge, unlockCredentials{Method: "password", Password: &wrong}, pick)
			if err != nil || retry.Code != "decrypt_failed" {
				t.Fatalf("错误口令 = %+v, %v", retry, err)
			}
			opened, err := d.unlockWithPicker(t.Context(), challenge.Challenge, unlockCredentials{Method: "password", Password: &password, UserName: "User"}, pick)
			if err != nil || opened.PagePrefix == "" || d.unlock != nil {
				t.Fatalf("解锁 = %+v, %v", opened, err)
			}
			info, err := d.Info(opened.PagePrefix)
			if err != nil || info.Security["encrypted"] != true || info.Security["state"] != "unlocked" {
				t.Fatalf("安全元数据 = %+v, %v", info.Security, err)
			}
			version, err := d.SelectVersion(opened.PagePrefix, 0, "v1")
			if err != nil || version.Pages[0].Width != 60 {
				t.Fatalf("解密视图历史版本 = %+v, %v", version, err)
			}
			if err := d.Close(version.PagePrefix); err != nil || d.unlockedView != nil {
				t.Fatalf("解密视图关闭 = %v", err)
			}
			if _, err := d.unlockWithPicker(t.Context(), challenge.Challenge, unlockCredentials{}, pick); !errors.Is(err, errUnlockExpired) {
				t.Fatalf("旧挑战未撤销: %v", err)
			}
		})
	}
}

func TestNativeUnlockCredentialFilesPreserveBytesAndRejectOversize(t *testing.T) {
	path := filepath.Join(t.TempDir(), "password.txt")
	value := "\ufeff  secret\n"
	if err := os.WriteFile(path, []byte(value), 0o600); err != nil {
		t.Fatal(err)
	}
	options, cancelled, err := unlockOptions(unlockCredentials{Method: "passwordFile"}, func(string) (string, error) { return path, nil })
	if err != nil || cancelled || string(options.Password) != value {
		t.Fatalf("口令文件 = %q, %v", options.Password, err)
	}
	clear(options.Password)
	if err := os.WriteFile(path, make([]byte, (64<<10)+1), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, _, err := unlockOptions(unlockCredentials{Method: "passwordFile"}, func(string) (string, error) { return path, nil }); !errors.Is(err, errUnlockCredentials) {
		t.Fatalf("过大口令文件 = %v", err)
	}
	if _, cancelled, err := unlockOptions(unlockCredentials{Method: "privateKey"}, func(string) (string, error) { return "", nil }); err != nil || !cancelled {
		t.Fatalf("取消私钥文件 = %v, %v", cancelled, err)
	}
}

func TestNativeSM2UnlockUsesNativePrivateKeyFileAndSupportsEncryptedPKCS8(t *testing.T) {
	key, err := sm2.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	certificate := &smx509.Certificate{SerialNumber: big.NewInt(1), Subject: pkix.Name{CommonName: "测试接收者"}, NotBefore: now.Add(-time.Hour), NotAfter: now.Add(time.Hour), KeyUsage: smx509.KeyUsageKeyEncipherment}
	der, err := smx509.CreateCertificate(rand.Reader, certificate, certificate, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	var encrypted bytes.Buffer
	if _, err := crypt.Encrypt(t.Context(), capabilityPackage(t, ""), &encrypted, crypt.EncryptOptions{Recipients: []crypt.EncryptRecipient{{UserName: "cert", Certificate: der}}}); err != nil {
		t.Fatal(err)
	}
	keyDER, err := pkcs8.MarshalPrivateKey(key, []byte("key-password"), pkcs8.DefaultOpts)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "private-key.pem")
	if err := os.WriteFile(path, pem.EncodeToMemory(&pem.Block{Type: "ENCRYPTED PRIVATE KEY", Bytes: keyDER}), 0o600); err != nil {
		t.Fatal(err)
	}
	d, challenge := capabilityDesktop(t, encrypted.Bytes())
	choose := func(method string) (string, error) {
		if method != "privateKey" {
			t.Fatalf("凭据选择类型 = %s", method)
		}
		return path, nil
	}
	retry, err := d.unlockWithPicker(t.Context(), challenge.Challenge, unlockCredentials{Method: "privateKey", PrivateKeyPassword: "wrong"}, choose)
	if err != nil || retry.Code != "invalid_credentials" {
		t.Fatalf("私钥错误口令 = %+v, %v", retry, err)
	}
	opened, err := d.unlockWithPicker(t.Context(), challenge.Challenge, unlockCredentials{Method: "privateKey", PrivateKeyPassword: "key-password", UserName: "cert"}, choose)
	if err != nil || opened.PagePrefix == "" {
		t.Fatalf("SM2 私钥解锁 = %+v, %v", opened, err)
	}
	if page, err := d.PageInfo(opened.PagePrefix, 0); err != nil || page.Width != 30 {
		t.Fatalf("SM2 解锁后页面 = %+v, %v", page, err)
	}
}

func TestNativePrintUsesPrintNotExportPermissionAndRevokesOldResources(t *testing.T) {
	d, opened := capabilityDesktop(t, capabilityPackage(t, `<Permissions><Export>false</Export><Print Printable="true" Copies="2"/></Permissions>`))
	job, err := d.preparePrint(t.Context(), opened.PagePrefix, []int{0})
	if err != nil {
		t.Fatal(err)
	}
	config, err := png.DecodeConfig(bytes.NewReader(job.images[0]))
	if err != nil || config.Width < 1 || !strings.Contains(job.html(), "size:30mm 20mm") || job.maxCopies != 2 || !strings.Contains(job.html(), "最多打印 2 份") {
		t.Fatalf("打印图像与物理尺寸 = %v, %v", config, err)
	}
	d.printing = job
	for _, path := range []string{"index.html", "print.js", "0.png"} {
		response := httptest.NewRecorder()
		d.servePrint(response, httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/reader-print/"+job.token+"/"+path, nil))
		if response.Code != http.StatusOK || response.Header().Get("Cache-Control") != "no-store" {
			t.Fatalf("打印资源 %s = %d", path, response.Code)
		}
	}
	if _, err := d.SelectVersion(opened.PagePrefix, 0, "v1"); err != nil {
		t.Fatal(err)
	}
	response := httptest.NewRecorder()
	d.servePrint(response, httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/reader-print/"+job.token+"/0.png", nil))
	if response.Code != http.StatusNotFound || d.printing != nil {
		t.Fatalf("旧打印内容未释放: %d", response.Code)
	}
	if err := d.PrintPrepared(job.token); !errors.Is(err, errPrintExpired) {
		t.Fatalf("旧打印任务 = %v", err)
	}
	for _, indices := range [][]int{nil, {0, 0}, {-1}, make([]int, 201)} {
		if _, err := d.preparePrint(t.Context(), d.inputPrefix, indices); !errors.Is(err, errPrintSelection) {
			t.Fatalf("非法打印选择 = %v", err)
		}
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err := d.preparePrint(ctx, d.inputPrefix, []int{0}); !errors.Is(err, context.Canceled) {
		t.Fatalf("打印取消 = %v", err)
	}
	job.expires = time.Now().Add(-time.Minute)
	d.printing = job
	response = httptest.NewRecorder()
	d.servePrint(response, httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/reader-print/"+job.token+"/index.html", nil))
	if response.Code != http.StatusNotFound {
		t.Fatalf("过期打印任务 = %d", response.Code)
	}
}

func TestNativePrintRejectsForbiddenPagesBeforePreparingOutput(t *testing.T) {
	d, opened := capabilityDesktop(t, capabilityPackage(t, `<Permissions><Print Printable="false"/></Permissions>`))
	if job, err := d.preparePrint(t.Context(), opened.PagePrefix, []int{0}); !errors.Is(err, reader.ErrPermissionDenied) || job != nil {
		t.Fatalf("打印权限 = %v, %v", job, err)
	}
}

func TestNativeVersionRejectsChangedSourceWithoutLosingCurrentSession(t *testing.T) {
	d, opened := capabilityDesktop(t, capabilityPackage(t, ""))
	replacement := filepath.Join(filepath.Dir(d.inputPath), "replacement.ofd")
	if err := os.WriteFile(replacement, []byte("changed"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(replacement, d.inputPath); err != nil {
		t.Fatal(err)
	}
	if _, err := d.SelectVersion(opened.PagePrefix, 0, "v1"); !errors.Is(err, errVersionSourceChanged) {
		t.Fatalf("源文件变化 = %v", err)
	}
	// 旧 Reader 仍持有原文件句柄；版本切换失败不得撤销其资源地址。
	if page, err := d.PageInfo(opened.PagePrefix, 0); err != nil || page.Width != 30 {
		t.Fatalf("变化检查破坏旧会话: %+v, %v", page, err)
	}
}

func TestNativeUnlockCancellationAndExpiredChallengesKeepOldDocument(t *testing.T) {
	d, opened := capabilityDesktop(t, capabilityPackage(t, ""))
	info, err := os.Stat(d.inputPath)
	if err != nil {
		t.Fatal(err)
	}
	challenge, err := d.challenge(d.inputPath, info)
	if err != nil {
		t.Fatal(err)
	}
	d.CancelUnlock(challenge.Challenge)
	choose := func(string) (string, error) { t.Fatal("无效解锁请求不能选择凭据文件"); return "", nil }
	if _, err := d.unlockWithPicker(t.Context(), challenge.Challenge, unlockCredentials{Method: "privateKey"}, choose); !errors.Is(err, errUnlockExpired) {
		t.Fatalf("取消挑战 = %v", err)
	}
	challenge, err = d.challenge(d.inputPath, info)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err := d.unlockWithPicker(ctx, challenge.Challenge, unlockCredentials{Method: "privateKey"}, choose); !errors.Is(err, context.Canceled) {
		t.Fatalf("取消上下文 = %v", err)
	}
	d.unlock.expires = time.Now().Add(-time.Minute)
	if _, err := d.unlockWithPicker(t.Context(), challenge.Challenge, unlockCredentials{Method: "privateKey"}, choose); !errors.Is(err, errUnlockExpired) {
		t.Fatalf("过期挑战 = %v", err)
	}
	if page, err := d.PageInfo(opened.PagePrefix, 0); err != nil || page.Width != 30 {
		t.Fatalf("取消影响旧文档 = %+v, %v", page, err)
	}
}

func TestNativeUnlockCancellationDuringCredentialDialogPreservesContextError(t *testing.T) {
	d, opened := capabilityDesktop(t, capabilityPackage(t, ""))
	info, err := os.Stat(d.inputPath)
	if err != nil {
		t.Fatal(err)
	}
	challenge, err := d.challenge(d.inputPath, info)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	result, err := d.unlockWithPicker(ctx, challenge.Challenge, unlockCredentials{Method: "privateKey"}, func(string) (string, error) {
		cancel()
		return "", context.Canceled
	})
	if !errors.Is(err, context.Canceled) || result != nil {
		t.Fatalf("取消不能转为凭据错误并重试: %+v, %v", result, err)
	}
	if page, err := d.PageInfo(opened.PagePrefix, 0); err != nil || page.Width != 30 {
		t.Fatalf("取消影响旧文档: %+v, %v", page, err)
	}
}
