package main

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/pem"
	"errors"
	"fmt"
	"io"
	"os"
	"time"
	"unicode/utf8"

	"github.com/emmansun/gmsm/pkcs8"
	"github.com/emmansun/gmsm/sm2"
	readersession "github.com/zc310/ofd/internal/reader/session"
	"github.com/zc310/ofd/pkg/crypt"
)

var errUnlockExpired = errors.New("解锁请求已过期，请重新打开文档")
var errUnlockCredentials = errors.New("凭据参数无效")

type unlockPending struct {
	token, path string
	info        os.FileInfo
	expires     time.Time
}

type unlockCredentials struct {
	Method             string  `json:"method"`
	Password           *string `json:"password"`
	UserName           string  `json:"user_name"`
	PrivateKeyPassword string  `json:"private_key_password"`
}

func (d *Desktop) challenge(path string, info os.FileInfo) (*openedDocument, error) {
	var token [32]byte
	if _, err := rand.Read(token[:]); err != nil {
		return nil, err
	}
	d.unlock = &unlockPending{token: hex.EncodeToString(token[:]), path: path, info: info, expires: time.Now().Add(5 * time.Minute)}
	return d.unlockReply("credentials_required", ""), nil
}

func (d *Desktop) unlockReply(code, message string) *openedDocument {
	return &openedDocument{Name: d.unlock.info.Name(), Challenge: d.unlock.token, Code: code, Message: message}
}

// CancelUnlock 只撤销对应的待解锁请求，不关闭正在阅读的旧文档。
func (d *Desktop) CancelUnlock(token string) {
	d.openMu.Lock()
	defer d.openMu.Unlock()
	if d.unlock != nil && d.unlock.token == token {
		d.unlock = nil
	}
}

// Unlock 从原生选定的路径创建只读解密视图；口令和私钥不写入配置或最近记录。
func (d *Desktop) Unlock(ctx context.Context, token string, credentials unlockCredentials) (*openedDocument, error) {
	return d.unlockWithPicker(ctx, token, credentials, func(method string) (string, error) {
		dialog := d.app.Dialog.OpenFile().SetTitle(d.tr("选择解锁凭据")).CanChooseFiles(true)
		if method == "privateKey" {
			dialog.AddFilter(d.tr("SM2 PKCS#8 私钥"), "*.pem;*.der;*.key")
		}
		return dialog.PromptForSingleSelection()
	})
}

func (d *Desktop) unlockWithPicker(ctx context.Context, token string, credentials unlockCredentials, choose func(string) (string, error)) (*openedDocument, error) {
	d.openMu.Lock()
	defer d.openMu.Unlock()
	pending := d.unlock
	if pending == nil || token != pending.token || time.Now().After(pending.expires) {
		return nil, errUnlockExpired
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	info, err := os.Stat(pending.path)
	if err != nil {
		return nil, err
	}
	if !os.SameFile(info, pending.info) || info.Size() != pending.info.Size() || !info.ModTime().Equal(pending.info.ModTime()) {
		d.unlock = nil
		return nil, errUnlockExpired
	}
	options, cancelled, err := unlockOptions(credentials, choose)
	if cancelErr := ctx.Err(); cancelErr != nil {
		clear(options.Password)
		if key, ok := options.PrivateKey.(*sm2.PrivateKey); ok {
			key.D.SetInt64(0)
		}
		return nil, cancelErr
	}
	if err != nil {
		return d.unlockReply("invalid_credentials", err.Error()), nil
	}
	if cancelled {
		d.unlock = nil
		return nil, nil //nolint:nilnil // 与取消原生打开对话框一致：空文档且无错误表示用户取消。
	}
	defer clear(options.Password)
	if key, ok := options.PrivateKey.(*sm2.PrivateKey); ok {
		defer key.D.SetInt64(0)
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if time.Now().After(pending.expires) {
		d.unlock = nil
		return nil, errUnlockExpired
	}
	info, err = os.Stat(pending.path)
	if err != nil {
		return nil, err
	}
	if !os.SameFile(info, pending.info) || info.Size() != pending.info.Size() || !info.ModTime().Equal(pending.info.ModTime()) {
		d.unlock = nil
		return nil, errUnlockExpired
	}
	options.Mode = crypt.ModeCompat
	options.Limits.MaxInputBytes = 256 << 20
	// 仅在文件未声明方案时，使用用户明确选择的凭据类型。
	options.Resolve = func(_ context.Context, request crypt.CredentialRequest) (crypt.Credentials, error) {
		result := crypt.Credentials{Password: options.Password, PrivateKey: options.PrivateKey, UserName: options.UserName}
		if request.Scheme == "" {
			if options.PrivateKey != nil {
				result.MissingScheme = crypt.CertificateScheme
			} else {
				result.MissingScheme = crypt.PasswordScheme
			}
		}
		return result, nil
	}
	view, err := crypt.Open(ctx, pending.path, crypt.OpenOptions{DecryptOptions: options})
	if err != nil {
		if errors.Is(err, crypt.ErrCredentialsRequired) || errors.Is(err, crypt.ErrDecryptFailed) {
			return d.unlockReply("decrypt_failed", "凭据不匹配，或加密内容已损坏"), nil
		}
		return nil, err
	}
	if err := ctx.Err(); err != nil {
		_ = view.Close()
		return nil, err
	}
	//nolint:contextcheck // Service.Open 是同步会话发布接口；解密已完成并检查取消。
	result, err := d.document.Open(view, readersession.Options{})
	if err != nil {
		_ = view.Close()
		return nil, err
	}
	old := d.unlockedView
	d.unlockedView = view
	if old != nil {
		_ = old.Close()
	}
	d.unlock = nil
	return d.acceptOpened(pending.path, info, result), nil
}

func unlockOptions(credentials unlockCredentials, choose func(string) (string, error)) (crypt.DecryptOptions, bool, error) {
	options := crypt.DecryptOptions{UserName: credentials.UserName}
	if len(credentials.UserName) > 4096 || len(credentials.PrivateKeyPassword) > 64<<10 {
		return options, false, errUnlockCredentials
	}
	switch credentials.Method {
	case "password":
		if credentials.Password == nil || len(*credentials.Password) > 64<<10 {
			return options, false, errUnlockCredentials
		}
		options.Password = append([]byte{}, []byte(*credentials.Password)...)
	case "passwordFile", "privateKey":
		path, err := choose(credentials.Method)
		defer releasePlatformCredential(path)
		if err != nil || path == "" {
			return options, path == "" && err == nil, err
		}
		limit := int64(64 << 10)
		if credentials.Method == "privateKey" {
			limit = 4 << 20
		}
		data, err := readCredentialFile(path, limit)
		if err != nil {
			return options, false, err
		}
		if credentials.Method == "passwordFile" {
			if !utf8.Valid(data) {
				clear(data)
				return options, false, errUnlockCredentials
			}
			options.Password = data // 保留空格、BOM、换行及空口令。
		} else {
			defer clear(data)
			password := []byte(credentials.PrivateKeyPassword)
			defer clear(password)
			options.PrivateKey, err = parseUnlockKey(data, password)
			if err != nil {
				return options, false, err
			}
		}
	default:
		return options, false, errUnlockCredentials
	}
	return options, false, nil
}

func readCredentialFile(path string, limit int64) ([]byte, error) {
	// 在打开之前拒绝目录、设备和管道，避免普通凭据读取被特殊文件阻塞。
	info, err := os.Stat(path)
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() || info.Size() > limit {
		return nil, errUnlockCredentials
	}
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	info, err = file.Stat()
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() || info.Size() > limit {
		return nil, errUnlockCredentials
	}
	data, err := io.ReadAll(io.LimitReader(file, limit+1))
	if err != nil || int64(len(data)) > limit {
		clear(data)
		return nil, errors.Join(errUnlockCredentials, err)
	}
	return data, nil
}

func parseUnlockKey(data, password []byte) (*sm2.PrivateKey, error) {
	der := data
	if bytes.HasPrefix(bytes.TrimSpace(data), []byte("-----BEGIN")) {
		block, rest := pem.Decode(data)
		if block == nil || (block.Type != "PRIVATE KEY" && block.Type != "ENCRYPTED PRIVATE KEY") || len(bytes.TrimSpace(rest)) != 0 {
			return nil, errUnlockCredentials
		}
		der = block.Bytes
		defer clear(der)
	}
	key, err := pkcs8.ParsePKCS8PrivateKeySM2(der, password)
	if err != nil {
		return nil, fmt.Errorf("%w: 无法解析 SM2 PKCS#8 私钥", errUnlockCredentials)
	}
	return key, nil
}
