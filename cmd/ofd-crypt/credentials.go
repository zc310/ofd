package main

import (
	"bytes"
	"context"
	"crypto"
	"encoding/pem"
	"fmt"
	"io"
	"os"

	"github.com/emmansun/gmsm/sm2"
	"github.com/emmansun/gmsm/smx509"
	"github.com/zc310/ofd/pkg/crypt"
)

type credentials struct {
	password   []byte
	key        crypto.Decrypter
	recipients []crypt.EncryptRecipient
	selected   map[string]crypt.Credentials
}

func (c *credentials) clear() {
	clear(c.password)
	for _, r := range c.recipients {
		clear(r.Password)
	}
	for _, value := range c.selected {
		clear(value.Password)
	}
}

func credentialPaths(o *options) []string {
	paths := []string{o.passwordFile, o.key, o.cert, o.inputPasswordFile, o.inputKey}
	paths = append(paths, o.protectedPaths...)
	for _, value := range append(append([]string{}, o.recipients...), o.passwordRecipients...) {
		_, path, err := namedFile(value)
		if err == nil {
			paths = append(paths, path)
		}
	}
	for _, spec := range credentialSelections(o) {
		for _, value := range spec.values {
			_, path, err := namedFile(value)
			if err == nil {
				paths = append(paths, path)
			}
		}
	}
	return paths
}

func readCredential(ctx context.Context, path string, stdin io.Reader, max int64) ([]byte, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if path == "-" {
		// An arbitrary caller-owned Reader cannot be interrupted or closed here.
		// Keep at most one pending stdin read. The CLI exits on cancellation;
		// a library test/embedding caller remains responsible for its reader.
		type result struct {
			data []byte
			err  error
		}
		ready := make(chan result)
		go func() {
			data, err := readBounded(stdin, max)
			select {
			case ready <- result{data, err}:
			case <-ctx.Done():
				clear(data)
			}
		}()
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case value := <-ready:
			if err := ctx.Err(); err != nil {
				clear(value.data)
				return nil, err
			}
			return value.data, value.err
		}
	}
	var reader io.Reader = stdin
	if path != "-" {
		file, err := os.Open(path)
		if err != nil {
			return nil, err
		}
		defer file.Close()
		reader = file
	}
	data, err := readBounded(reader, max)
	if ctx.Err() != nil {
		clear(data)
		return nil, ctx.Err()
	}
	return data, err
}

func readBounded(reader io.Reader, max int64) ([]byte, error) {
	data, err := io.ReadAll(io.LimitReader(reader, max+1))
	if err != nil {
		clear(data)
		return nil, err
	}
	if int64(len(data)) > max {
		clear(data)
		return nil, fmt.Errorf("凭据文件超过大小限制")
	}
	if data == nil {
		data = []byte{}
	}
	return data, nil
}

func pemOrDER(data []byte, kind string) ([]byte, error) {
	if !bytes.HasPrefix(bytes.TrimSpace(data), []byte("-----BEGIN")) {
		return data, nil
	}
	block, rest := pem.Decode(data)
	if block == nil || block.Type != kind || len(bytes.TrimSpace(rest)) != 0 {
		return nil, fmt.Errorf("需要单个 %s PEM 块或 DER 文件", kind)
	}
	return block.Bytes, nil
}

func loadCredentials(ctx context.Context, o *options, stdin io.Reader) (result *credentials, err error) {
	c := &credentials{selected: map[string]crypt.Credentials{}}
	defer func() {
		if err != nil {
			c.clear()
		}
	}()
	usedStdin := false
	password := func(file string) ([]byte, error) {
		if file == "-" {
			if usedStdin {
				return nil, usage("标准输入只能供一个接收者读取口令")
			}
			usedStdin = true
		}
		return readCredential(ctx, file, stdin, 64<<10)
	}
	certificate := func(file string) ([]byte, error) {
		data, err := readCredential(ctx, file, stdin, 4<<20)
		if err != nil {
			return nil, err
		}
		der, err := pemOrDER(data, "CERTIFICATE")
		if err != nil {
			return nil, err
		}
		if _, err := smx509.ParseCertificate(der); err != nil {
			return nil, fmt.Errorf("证书解析失败: %w", err)
		}
		return der, nil
	}
	loadKey := func(path string) (crypto.Decrypter, error) {
		data, err := readCredential(ctx, path, stdin, 4<<20)
		if err != nil {
			return nil, err
		}
		defer clear(data)
		der, err := pemOrDER(data, "PRIVATE KEY")
		if err != nil {
			return nil, err
		}
		defer clear(der)
		key, err := smx509.ParsePKCS8PrivateKey(der)
		if err != nil {
			return nil, fmt.Errorf("SM2 PKCS#8 私钥解析失败: %w", err)
		}
		sm2Key, ok := key.(*sm2.PrivateKey)
		if !ok {
			return nil, fmt.Errorf("私钥不是 SM2 类型")
		}
		return sm2Key, nil
	}
	if o.action != "encrypt" || o.appendLayer {
		passwordFile, keyFile := o.passwordFile, o.key
		if o.action == "encrypt" {
			passwordFile, keyFile = o.inputPasswordFile, o.inputKey
		}
		if passwordFile != "" {
			c.password, err = password(passwordFile)
			if err != nil {
				return nil, err
			}
		}
		if keyFile != "" {
			c.key, err = loadKey(keyFile)
			if err != nil {
				return nil, err
			}
		}
		for _, spec := range credentialSelections(o) {
			for _, value := range spec.values {
				selector, file, parseErr := namedFile(value)
				if parseErr != nil {
					return nil, parseErr
				}
				credential := crypt.Credentials{}
				if spec.password {
					credential.Password, err = password(file)
				} else {
					credential.PrivateKey, err = loadKey(file)
				}
				if err != nil {
					return nil, err
				}
				c.selected[spec.kind+selector] = credential
			}
		}
		if o.action != "encrypt" {
			return c, nil
		}
	}
	add := func(name, file string, isPassword bool) error {
		r := crypt.EncryptRecipient{UserName: name, UserType: o.role}
		var err error
		if isPassword {
			r.Password, err = password(file)
		} else {
			r.Certificate, err = certificate(file)
		}
		if err != nil {
			return err
		}
		c.recipients = append(c.recipients, r)
		return nil
	}
	if o.passwordFile != "" {
		err = add(o.user, o.passwordFile, true)
	}
	if o.cert != "" {
		err = add(o.user, o.cert, false)
	}
	for _, values := range []struct {
		items    []string
		password bool
	}{{o.passwordRecipients, true}, {o.recipients, false}} {
		for _, value := range values.items {
			name, file, parseErr := namedFile(value)
			if parseErr != nil {
				return nil, parseErr
			}
			if err := add(name, file, values.password); err != nil {
				return nil, err
			}
		}
	}
	return c, err
}

type credentialSelection struct {
	values   []string
	kind     string
	password bool
}

func credentialSelections(o *options) []credentialSelection {
	return []credentialSelection{{o.passwordFor, "operation:", true}, {o.keyFor, "operation:", false}, {o.passwordForSeed, "seed:", true}, {o.keyForSeed, "seed:", false}}
}
func hasCredentials(o *options) bool {
	return o.passwordFile != "" || o.key != "" || len(o.passwordFor)+len(o.keyFor)+len(o.passwordForSeed)+len(o.keyForSeed) > 0
}

func hasInputCredentials(o *options) bool {
	return o.inputPasswordFile != "" || o.inputKey != "" || len(o.passwordFor)+len(o.keyFor)+len(o.passwordForSeed)+len(o.keyForSeed) > 0
}

func decryptOptions(o *options, c *credentials) crypt.DecryptOptions {
	user := ""
	if o.userSet {
		user = o.user
	}
	if o.action == "encrypt" {
		user = o.inputUser
	}
	scheme := ""
	if o.missingScheme == "password" {
		scheme = crypt.PasswordScheme
	}
	if o.missingScheme == "certificate" {
		scheme = crypt.CertificateScheme
	}
	opt := crypt.DecryptOptions{Mode: crypt.Mode(o.mode), Limits: o.limits, Password: c.password, PrivateKey: c.key, UserName: user, MissingScheme: scheme}
	if len(c.selected) > 0 {
		opt.Resolve = func(_ context.Context, r crypt.CredentialRequest) (crypt.Credentials, error) {
			credential, ok := c.selected["seed:"+r.DecryptSeedLoc]
			if !ok {
				credential, ok = c.selected["operation:"+r.OperationID]
			}
			if !ok {
				credential = crypt.Credentials{Password: c.password, PrivateKey: c.key}
			}
			if credential.Password == nil && credential.PrivateKey == nil {
				return credential, crypt.ErrCredentialsRequired
			}
			credential.UserName = user
			if r.Scheme == "" {
				credential.MissingScheme = scheme
			}
			return credential, nil
		}
	}
	return opt
}
