package crypt_test

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/pem"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/zc310/ofd/pkg/crypt"
	"github.com/zc310/ofd/pkg/sign"
)

// Opt in with a classpath prepared from testdata/crypt/ofdrw/pom.xml. No Java
// dependencies are downloaded by ordinary Go tests.
func TestOFDRW240Interoperability(t *testing.T) {
	classpath := os.Getenv("OFD_CRYPTO_OFDRW_CLASSPATH")
	if classpath == "" {
		t.Skip("set OFD_CRYPTO_OFDRW_CLASSPATH to the pinned ofdrw 2.4.0 dependencies")
	}
	for _, tool := range []string{"java", "javac"} {
		if _, err := exec.LookPath(tool); err != nil {
			t.Fatal(err)
		}
	}
	dir := t.TempDir()
	invoke := func(tool string, args ...string) ([]byte, error) {
		ctx, cancel := context.WithTimeout(t.Context(), 45*time.Second)
		defer cancel()
		cmd := exec.CommandContext(ctx, tool, args...)
		return cmd.CombinedOutput()
	}
	command := func(t *testing.T, tool string, args ...string) {
		t.Helper()
		data, err := invoke(tool, args...)
		if err != nil {
			t.Fatalf("%s failed: %v\n%s", tool, err, data)
		}
		if tool == "java" && !strings.Contains(string(data), "ofdrw 2.4.0") {
			t.Fatalf("ofdrw version was not verified: %s", data)
		}
	}
	command(t, "javac", "-encoding", "UTF-8", "-cp", classpath, "-d", dir, "../../testdata/crypt/ofdrw/Interop.java")
	javaCP := dir + string(os.PathListSeparator) + classpath
	v := readVectors(t)
	cert, err := base64.StdEncoding.DecodeString(v.SM2.Certificate)
	if err != nil {
		t.Fatal(err)
	}
	key, _ := pem.Decode([]byte(v.SM2.PrivateKey))
	if key == nil {
		t.Fatal("missing test private key")
	}
	password := []byte("中文互通🔐")
	write := func(name string, data []byte) string {
		t.Helper()
		path := filepath.Join(dir, name)
		if err := os.WriteFile(path, data, 0600); err != nil {
			t.Fatal(err)
		}
		return path
	}
	passwordFile := write("password.txt", password)
	certFile := write("certificate.der", cert)
	keyFile := write("private-key.der", key.Bytes)
	for _, scheme := range []string{"password", "certificate"} {
		for _, scope := range []string{"all", "partial"} {
			t.Run(scheme+"/"+scope, func(t *testing.T) {
				originals := minimalOriginals()
				input := write(scheme+"-"+scope+"-input.ofd", archive(t, originals))
				credential := passwordFile
				recipient := crypt.EncryptRecipient{UserName: "interop-user", Password: password}
				decryptOpt := crypt.DecryptOptions{Mode: crypt.ModeCompat, MissingScheme: crypt.PasswordScheme, Password: password, UserName: "interop-user"}
				decryptCredential := passwordFile
				if scheme == "certificate" {
					credential, decryptCredential = certFile, keyFile
					recipient = crypt.EncryptRecipient{UserName: "interop-user", Certificate: cert}
					decryptOpt = crypt.DecryptOptions{Mode: crypt.ModeCompat, MissingScheme: crypt.CertificateScheme, PrivateKey: privateKey(t), UserName: "interop-user"}
				}
				javaEncrypted := filepath.Join(dir, scheme+"-"+scope+"-java-encrypted.ofd")
				javaArgs := []string{"-cp", javaCP, "Interop", "encrypt-" + scheme, input, javaEncrypted, "interop-user", credential}
				var selected []string
				if scope == "partial" {
					selected = []string{"Doc_0/data.bin"}
					javaArgs = append(javaArgs, "/Doc_0/data.bin")
				}
				command(t, "java", javaArgs...)
				var decrypted bytes.Buffer
				if _, err := crypt.Decrypt(t.Context(), javaEncrypted, &decrypted, decryptOpt); err != nil {
					t.Fatal("ofdrw -> Go:", err)
				}
				checkOriginals(t, decrypted.Bytes(), originals)
				for _, plainMap := range []bool{false, true} {
					var encrypted bytes.Buffer
					if _, err := crypt.Encrypt(t.Context(), input, &encrypted, crypt.EncryptOptions{Recipients: []crypt.EncryptRecipient{recipient}, Entries: selected, PlaintextMap: plainMap}); err != nil {
						t.Fatal(err)
					}
					goEncrypted := write(scheme+"-"+scope+"-go-encrypted.ofd", encrypted.Bytes())
					javaDecrypted := filepath.Join(dir, scheme+"-"+scope+"-java-decrypted.ofd")
					javaArgs := []string{"-cp", javaCP, "Interop", "decrypt-" + scheme, goEncrypted, javaDecrypted, "interop-user", decryptCredential}
					if plainMap {
						data, err := invoke("java", javaArgs...)
						if err != nil && strings.Contains(string(data), "org.bouncycastle.crypto.DataLengthException: last block incomplete in decryption") {
							t.Log("ofdrw 2.4.0 known limitation: plaintext map length triggers unchecked DataLengthException")
							continue
						}
						if err != nil {
							t.Fatalf("plaintext map: %v\n%s", err, data)
						}
					} else {
						command(t, "java", javaArgs...)
					}
					data, err := os.ReadFile(javaDecrypted)
					if err != nil {
						t.Fatal(err)
					}
					checkOriginals(t, data, originals)
				}
			})
		}
	}
	t.Run("signed-document/password", func(t *testing.T) {
		data, err := os.ReadFile("../../testdata/ofdrw/999.ofd")
		if errors.Is(err, os.ErrNotExist) {
			t.Skip("missing signed fixture")
		}
		if err != nil {
			t.Fatal(err)
		}
		originals := unpack(t, data)
		before, err := sign.Verify(t.Context(), data, sign.VerificationOptions{})
		if err != nil {
			t.Fatal(err)
		}
		input := write("signed-input.ofd", data)
		javaEncrypted := filepath.Join(dir, "signed-java-encrypted.ofd")
		command(t, "java", "-cp", javaCP, "Interop", "encrypt-password", input, javaEncrypted, "interop-user", passwordFile)
		var plain, encrypted bytes.Buffer
		if _, err := crypt.Decrypt(t.Context(), javaEncrypted, &plain, crypt.DecryptOptions{Mode: crypt.ModeCompat, MissingScheme: crypt.PasswordScheme, Password: password}); err != nil {
			t.Fatal(err)
		}
		checkOriginals(t, plain.Bytes(), originals)
		if _, err := crypt.Encrypt(t.Context(), data, &encrypted, crypt.EncryptOptions{Recipients: []crypt.EncryptRecipient{{UserName: "interop-user", Password: password}}}); err != nil {
			t.Fatal(err)
		}
		goEncrypted := write("signed-go-encrypted.ofd", encrypted.Bytes())
		javaDecrypted := filepath.Join(dir, "signed-java-decrypted.ofd")
		command(t, "java", "-cp", javaCP, "Interop", "decrypt-password", goEncrypted, javaDecrypted, "interop-user", passwordFile)
		javaPlain, err := os.ReadFile(javaDecrypted)
		if err != nil {
			t.Fatal(err)
		}
		checkOriginals(t, javaPlain, originals)
		for _, restored := range [][]byte{plain.Bytes(), javaPlain} {
			after, err := sign.Verify(t.Context(), restored, sign.VerificationOptions{})
			if err != nil || len(after) != len(before) || !after[0].DigestValid || !after[0].Result.Valid {
				t.Fatalf("signature verification changed: %+v, %v", after, err)
			}
		}
	})
}
