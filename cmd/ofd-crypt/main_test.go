package main

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/klauspost/compress/zip"
	"github.com/zc310/ofd/internal/core"
	"github.com/zc310/ofd/internal/version"
	"github.com/zc310/ofd/pkg/crypt"
)

func cliFixture(t *testing.T) (string, string) {
	t.Helper()
	dir := t.TempDir()
	input := filepath.Join(dir, "input.ofd")
	var data bytes.Buffer
	z := zip.NewWriter(&data)
	for name, content := range map[string]string{"OFD.xml": `<OFD xmlns="http://www.ofdspec.org/2016" Version="1.0" DocType="OFD"><DocBody><DocRoot>Doc/Document.xml</DocRoot></DocBody></OFD>`, "Doc/Document.xml": `<Document xmlns="http://www.ofdspec.org/2016"><Pages/></Document>`, "Doc/data.bin": "original\x00bytes"} {
		w, err := z.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := io.WriteString(w, content); err != nil {
			t.Fatal(err)
		}
	}
	if err := z.Close(); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(input, data.Bytes(), 0600); err != nil {
		t.Fatal(err)
	}
	password := filepath.Join(dir, "password.txt")
	if err := os.WriteFile(password, []byte("secret\n"), 0600); err != nil {
		t.Fatal(err)
	}
	return input, password
}

func call(t *testing.T, args []string, stdin string, want int) (string, string) {
	t.Helper()
	var out, diag bytes.Buffer
	code := run(t.Context(), args, strings.NewReader(stdin), &out, &diag)
	if code != want {
		t.Fatalf("%v: code=%d want=%d,stdout=%s,stderr=%s", args, code, want, &out, &diag)
	}
	return out.String(), diag.String()
}

func TestCLIEncryptInspectDecryptPreservesBytesAndSeparatesReports(t *testing.T) {
	input, pw := cliFixture(t)
	dir := filepath.Dir(input)
	encrypted := filepath.Join(dir, "encrypted.ofd")
	plain := filepath.Join(dir, "plain.ofd")
	out, diag := call(t, []string{"encrypt", "--password-file", pw, "--user", "用户", "--format", "json", "-o", encrypted, input}, "", 0)
	if diag != "" || !json.Valid([]byte(out)) {
		t.Fatalf("stdout=%s stderr=%s", out, diag)
	}
	var report crypt.EncryptReport
	if err := json.Unmarshal([]byte(out), &report); err != nil {
		t.Fatal(err)
	}
	if report.EncryptedEntries != 3 {
		t.Fatalf("report=%+v", report)
	}
	for _, format := range []string{"text", "markdown", "json"} {
		out, diag := call(t, []string{"inspect", "--format", format, encrypted}, "", 0)
		if diag != "" || (!strings.Contains(out, "未解锁") && !strings.Contains(out, `"state": "locked"`)) {
			t.Fatalf("%s=%s stderr=%s", format, out, diag)
		}
	}
	call(t, []string{"decrypt", "--password-file", "-", "--user", "用户", "-o", plain, encrypted}, "secret\n", 0)
	before, err := core.OpenFile(input)
	if err != nil {
		t.Fatal(err)
	}
	defer before.Close()
	after, err := core.OpenFile(plain)
	if err != nil {
		t.Fatal(err)
	}
	defer after.Close()
	for _, entry := range before.Entries() {
		a, err := before.Read(entry.Path)
		if err != nil {
			t.Fatal(err)
		}
		b, err := after.Read(entry.Path)
		if err != nil || !bytes.Equal(a, b) {
			t.Fatalf("entry %s changed: %v", entry.Path, err)
		}
	}
	if info, err := crypt.Inspect(t.Context(), plain, crypt.InspectOptions{}); err != nil || info.Encrypted {
		t.Fatalf("info=%+v err=%v", info, err)
	}
	if stat, err := os.Stat(plain); err != nil || stat.Mode().Perm() != 0600 {
		t.Fatalf("plaintext permissions=%v err=%v", stat, err)
	}
}

func TestCLIFailedDecryptPreservesExistingOutputAndCleansTemporaryFile(t *testing.T) {
	input, pw := cliFixture(t)
	dir := filepath.Dir(input)
	enc := filepath.Join(dir, "enc.ofd")
	out := filepath.Join(dir, "out.ofd")
	call(t, []string{"encrypt", "--password-file", pw, "-o", enc, input}, "", 0)
	if err := os.WriteFile(out, []byte("existing output"), 0600); err != nil {
		t.Fatal(err)
	}
	_, diag := call(t, []string{"decrypt", "--password-file", "-", "--force", "-o", out, enc}, "wrong", 1)
	if strings.Contains(diag, "wrong") {
		t.Fatal("password leaked")
	}
	data, err := os.ReadFile(out)
	if err != nil || string(data) != "existing output" {
		t.Fatalf("output overwritten: %q %v", data, err)
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		if strings.HasPrefix(entry.Name(), ".ofd-crypt-") {
			t.Fatal("temporary plaintext output leaked")
		}
	}
	call(t, []string{"decrypt", "--password-file", pw, "--force", "-o", out, enc}, "", 0)
}

func TestCLIProtectsInputCredentialAliasesAndExistingOutput(t *testing.T) {
	input, pw := cliFixture(t)
	dir := filepath.Dir(input)
	for _, out := range []string{input, pw, dir} {
		call(t, []string{"encrypt", "--password-file", pw, "--force", "-o", out, input}, "", 2)
	}
	alias := filepath.Join(dir, "alias.ofd")
	if err := os.Link(input, alias); err != nil {
		t.Fatal(err)
	}
	call(t, []string{"encrypt", "--password-file", pw, "--force", "-o", alias, input}, "", 2)
	link := filepath.Join(dir, "symlink.ofd")
	if err := os.Symlink(input, link); err != nil {
		t.Fatal(err)
	}
	call(t, []string{"encrypt", "--password-file", pw, "--force", "-o", link, input}, "", 2)
	out := filepath.Join(dir, "existing.ofd")
	if err := os.WriteFile(out, []byte("keep"), 0600); err != nil {
		t.Fatal(err)
	}
	call(t, []string{"encrypt", "--password-file", pw, "-o", out, input}, "", 2)
}

func TestCLIUsageErrorsAndVersionAcrossSubcommands(t *testing.T) {
	input, pw := cliFixture(t)
	out := filepath.Join(filepath.Dir(input), "out.ofd")
	for _, args := range [][]string{{}, {"unknown"}, {"inspect"}, {"decrypt", "-o", out, input}, {"encrypt", "-o", out, input}, {"encrypt", "--password-file", pw, "--cert", pw, "-o", out, input}, {"encrypt", "--password-file", pw, "--entry", "../escape", "-o", out, input}, {"encrypt", "--password-file", pw, "--keep-root", "--entry", "OFD.xml", "-o", out, input}, {"decrypt", "--password-file", pw, "--missing-scheme", "password", "-o", out, input}, {"inspect", "--format", "bad", input}, {"inspect", "--max-entries", "-1", input}, {"encrypt", "--password-recipient", "a=-", "--password-recipient", "b=-", "-o", out, input}} {
		call(t, args, "", 2)
	}
	for _, action := range []string{"", "inspect", "encrypt", "decrypt"} {
		for _, flag := range []string{"--version", "-v"} {
			args := []string{flag}
			if action != "" {
				args = []string{action, flag}
			}
			text, diag := call(t, args, "", 0)
			if strings.TrimSpace(text) != version.Version || diag != "" {
				t.Fatalf("version=%q diag=%q", text, diag)
			}
		}
	}
	for _, action := range []string{"inspect", "encrypt", "decrypt"} {
		text, diag := call(t, []string{action, "--help"}, "", 0)
		if !strings.Contains(text, "Usage:") || diag != "" {
			t.Fatalf("help=%q diag=%q", text, diag)
		}
	}
}

func TestCLIMultiplePasswordRecipientsAndCertificateKeyFiles(t *testing.T) {
	input, pw := cliFixture(t)
	dir := filepath.Dir(input)
	enc := filepath.Join(dir, "enc.ofd")
	plain := filepath.Join(dir, "plain.ofd")
	call(t, []string{"encrypt", "--password-recipient", "甲=" + pw, "--password-recipient", "乙=-", "--entry", "Doc/data.bin", "--keep-root", "--plaintext-map", "-o", enc, input}, "second", 0)
	call(t, []string{"decrypt", "--password-file", "-", "--user", "乙", "-o", plain, enc}, "second", 0)
	data, err := os.ReadFile("../../testdata/crypt/vectors.json")
	if errors.Is(err, os.ErrNotExist) {
		t.Skip("missing crypto vectors")
	}
	if err != nil {
		t.Fatal(err)
	}
	var vectors struct {
		SM2 struct {
			Key  string `json:"private_key_pem"`
			Cert string `json:"certificate_der"`
		}
	}
	if err := json.Unmarshal(data, &vectors); err != nil {
		t.Fatal(err)
	}
	cert, err := base64.StdEncoding.DecodeString(vectors.SM2.Cert)
	if err != nil {
		t.Fatal(err)
	}
	certPath, keyPath := filepath.Join(dir, "cert.der"), filepath.Join(dir, "key.pem")
	if err := os.WriteFile(certPath, cert, 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(keyPath, []byte(vectors.SM2.Key), 0600); err != nil {
		t.Fatal(err)
	}
	call(t, []string{"encrypt", "--recipient", "one=" + certPath, "--recipient", "two=" + certPath, "--force", "-o", enc, input}, "", 0)
	call(t, []string{"decrypt", "--key", keyPath, "--user", "two", "--force", "-o", plain, enc}, "", 0)
}

func TestAtomicOutputCancellationAndLateFailureNeverPublish(t *testing.T) {
	input, _ := cliFixture(t)
	output := filepath.Join(filepath.Dir(input), "result.ofd")
	o := &options{input: input, output: output}
	failure := errors.New("late failure")
	if err := atomicOutput(t.Context(), o, func(w io.Writer) error { _, _ = io.WriteString(w, "plaintext"); return failure }); !errors.Is(err, failure) {
		t.Fatal(err)
	}
	if _, err := os.Stat(output); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("partial published: %v", err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	if err := atomicOutput(ctx, o, func(w io.Writer) error { _, _ = io.WriteString(w, "plaintext"); cancel(); return nil }); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	if _, err := os.Stat(output); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("cancelled output published: %v", err)
	}
	ctx, cancel = context.WithCancel(t.Context())
	cancel()
	var out, diag bytes.Buffer
	if code := run(ctx, []string{"inspect", input}, strings.NewReader(""), &out, &diag); code != 130 {
		t.Fatalf("cancel code=%d: %s", code, &diag)
	}
}

type notifiedReader struct {
	reader  io.Reader
	started chan struct{}
}

func (r notifiedReader) Read(p []byte) (int, error) {
	select {
	case r.started <- struct{}{}:
	default:
	}
	return r.reader.Read(p)
}

func TestPasswordStandardInputCanBeCancelledWhileBlocked(t *testing.T) {
	input, _ := cliFixture(t)
	output := filepath.Join(filepath.Dir(input), "cancelled.ofd")
	r, w := io.Pipe()
	defer r.Close()
	defer w.Close()
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	started := make(chan struct{}, 1)
	done := make(chan int, 1)
	go func() {
		done <- run(ctx, []string{"encrypt", "--password-file", "-", "-o", output, input}, notifiedReader{r, started}, io.Discard, io.Discard)
	}()
	select {
	case <-started:
	case <-time.After(2 * time.Second):
		t.Fatal("stdin read did not start")
	}
	cancel()
	select {
	case code := <-done:
		if code != 130 {
			t.Fatalf("cancel exit=%d", code)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("blocked stdin did not cancel")
	}
	if _, err := os.Stat(output); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("cancelled output published: %v", err)
	}
}

func combinedOperations(t *testing.T, input string, firstPassword, secondPassword []byte) []byte {
	t.Helper()
	unpack := func(data []byte) map[string]string {
		t.Helper()
		p, err := core.OpenBytes(data)
		if err != nil {
			t.Fatal(err)
		}
		defer p.Close()
		files := map[string]string{}
		for _, e := range p.Entries() {
			if e.IsDir {
				continue
			}
			b, err := p.Read(e.Path)
			if err != nil {
				t.Fatal(err)
			}
			files[e.Path] = string(b)
		}
		return files
	}
	var first, second bytes.Buffer
	for _, item := range []struct {
		password []byte
		entry    string
		out      *bytes.Buffer
	}{{firstPassword, "Doc/Document.xml", &first}, {secondPassword, "Doc/data.bin", &second}} {
		if _, err := crypt.Encrypt(t.Context(), input, item.out, crypt.EncryptOptions{Recipients: []crypt.EncryptRecipient{{UserName: "User", Password: item.password}}, Entries: []string{item.entry}, PlaintextMap: true}); err != nil {
			t.Fatal(err)
		}
	}
	a, b := unpack(first.Bytes()), unpack(second.Bytes())
	extract := func(root string) string {
		start := strings.Index(root, "<ofd:EncryptInfo")
		end := strings.Index(root, "</ofd:EncryptInfo>") + len("</ofd:EncryptInfo>")
		return root[start:end]
	}
	secondInfo := strings.ReplaceAll(strings.ReplaceAll(extract(b["Encryptions.xml"]), `ID="enc1"`, `ID="second"`), "/Encrypt_1/", "/Second/")
	for name, data := range b {
		if !strings.HasPrefix(name, "Encrypt_1/") {
			continue
		}
		if strings.HasSuffix(name, "decryptseed.dat") || strings.HasSuffix(name, "entriesmap.dat") {
			data = strings.ReplaceAll(strings.ReplaceAll(data, `ID="enc1"`, `ID="second"`), "/Encrypt_1/", "/Second/")
		}
		a[strings.Replace(name, "Encrypt_1/", "Second/", 1)] = data
	}
	delete(a, "Doc/data.bin")
	a["Encryptions.xml"] = `<ofd:Encryptions xmlns:ofd="http://www.ofdspec.org/2016">` + extract(a["Encryptions.xml"]) + secondInfo + `</ofd:Encryptions>`
	var result bytes.Buffer
	archive := zip.NewWriter(&result)
	for name, data := range a {
		w, err := archive.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := io.WriteString(w, data); err != nil {
			t.Fatal(err)
		}
	}
	if err := archive.Close(); err != nil {
		t.Fatal(err)
	}
	return result.Bytes()
}

func TestCLIPerOperationAndSeedCredentialsAndPartialInspection(t *testing.T) {
	input, firstFile := cliFixture(t)
	dir := filepath.Dir(input)
	secondFile := filepath.Join(dir, "second-password.txt")
	if err := os.WriteFile(secondFile, []byte("second"), 0600); err != nil {
		t.Fatal(err)
	}
	enc := filepath.Join(dir, "multi.ofd")
	if err := os.WriteFile(enc, combinedOperations(t, input, []byte("secret\n"), []byte("second")), 0600); err != nil {
		t.Fatal(err)
	}
	for _, format := range []string{"text", "markdown", "json"} {
		out, _ := call(t, []string{"inspect", "--format", format, "--password-for", "enc1=" + firstFile, enc}, "", 0)
		if format == "json" {
			var info crypt.Info
			if err := json.Unmarshal([]byte(out), &info); err != nil {
				t.Fatal(err)
			}
			if info.State != "partially_unlocked" {
				t.Fatalf("info=%+v", info)
			}
		} else if !strings.Contains(out, "部分解锁") {
			t.Fatalf("partial inspection=%s", out)
		}
	}
	plain := filepath.Join(dir, "multi-plain.ofd")
	call(t, []string{"decrypt", "--password-for", "enc1=" + firstFile, "--password-for-seed", "Second/decryptseed.dat=" + secondFile, "-o", plain, enc}, "", 0)
	p, err := core.OpenFile(plain)
	if err != nil {
		t.Fatal(err)
	}
	defer p.Close()
	data, err := p.Read("Doc/data.bin")
	if err != nil || string(data) != "original\x00bytes" {
		t.Fatalf("data=%q err=%v", data, err)
	}
	call(t, []string{"decrypt", "--password-for", "enc1=" + firstFile, "--force", "-o", plain, enc}, "", 1)
	call(t, []string{"decrypt", "--password-for", "enc1=" + firstFile, "--key-for", "enc1=" + secondFile, "-o", filepath.Join(dir, "unused.ofd"), enc}, "", 2)
}
