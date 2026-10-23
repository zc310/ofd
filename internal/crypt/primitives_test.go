package crypt

import (
	"bytes"
	"context"
	"crypto"
	"crypto/cipher"
	"crypto/ecdsa"
	"crypto/elliptic"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"os"
	"testing"

	"github.com/emmansun/gmsm/sm2"
	"github.com/emmansun/gmsm/sm4"
)

func unhex(t *testing.T, value string) []byte {
	t.Helper()
	data, err := hex.DecodeString(value)
	if err != nil {
		t.Fatal(err)
	}
	return data
}

type publicOnlyDecrypter struct{ public crypto.PublicKey }

func (k publicOnlyDecrypter) Public() crypto.PublicKey { return k.public }
func (publicOnlyDecrypter) Decrypt(io.Reader, []byte, crypto.DecrypterOpts) ([]byte, error) {
	return nil, errors.New("公钥预检不应调用解密服务")
}

func TestSM2PublicRejectsOtherCurvesIncludingNISTP256(t *testing.T) {
	for _, curve := range []elliptic.Curve{elliptic.P256(), elliptic.P384(), sm2.P256()} {
		t.Run(curve.Params().Name, func(t *testing.T) {
			public := &ecdsa.PublicKey{Curve: curve, X: curve.Params().Gx, Y: curve.Params().Gy}
			got, err := sm2Public(publicOnlyDecrypter{public})
			if curve == sm2.P256() {
				if err != nil || got != public {
					t.Fatalf("有效 SM2 公钥被拒绝: %v", err)
				}
			} else if err == nil || got != nil {
				t.Fatalf("非 SM2 曲线被接受: %s", curve.Params().Name)
			}
		})
	}
}

func TestOpenSSLPasswordAndCBCVectors(t *testing.T) {
	data, err := os.ReadFile("../../testdata/crypt/vectors.json")
	if errors.Is(err, os.ErrNotExist) {
		t.Skip("missing crypto vectors")
	}
	if err != nil {
		t.Fatal(err)
	}
	var vectors struct {
		FEK, IV   string
		Passwords []struct {
			Password, KEK string
			Wrapped       string `json:"wrapped_fek"`
		}
		CBC []struct{ Plaintext, Ciphertext string }
	}
	if err := json.Unmarshal(data, &vectors); err != nil {
		t.Fatal(err)
	}
	fek, iv := unhex(t, vectors.FEK), unhex(t, vectors.IV)
	for _, item := range vectors.Passwords {
		kek := passwordKEK([]byte(item.Password))
		if !bytes.Equal(kek, unhex(t, item.KEK)) {
			t.Fatalf("KDF(%q) = %x", item.Password, kek)
		}
		wrapped, err := encryptCBC(t.Context(), fek, kek, iv)
		if err != nil || !bytes.Equal(wrapped, unhex(t, item.Wrapped)) {
			t.Fatalf("OpenSSL wrapping mismatch: %x, %v", wrapped, err)
		}
		plain, err := decryptCBC(t.Context(), unhex(t, item.Wrapped), kek, iv)
		if err != nil || !bytes.Equal(plain, fek) {
			t.Fatalf("wrapped FEK: %x, err=%v", plain, err)
		}
	}
	for _, item := range vectors.CBC {
		ciphertext, err := encryptCBC(t.Context(), unhex(t, item.Plaintext), fek, iv)
		if err != nil || !bytes.Equal(ciphertext, unhex(t, item.Ciphertext)) {
			t.Fatalf("OpenSSL CBC encryption mismatch: %x, %v", ciphertext, err)
		}
		plain, err := decryptCBC(t.Context(), unhex(t, item.Ciphertext), fek, iv)
		if err != nil || !bytes.Equal(plain, unhex(t, item.Plaintext)) {
			t.Fatalf("CBC plaintext=%x, err=%v", plain, err)
		}
	}
}

func TestCBCRejectsEveryInvalidPaddingLengthAndMismatchedPadding(t *testing.T) {
	key, iv := make([]byte, 16), make([]byte, 16)
	block, _ := sm4.NewCipher(key)
	for padding := 0; padding < 256; padding++ {
		plain := bytes.Repeat([]byte{byte(padding)}, 16)
		if padding >= 1 && padding <= 16 {
			if padding == 1 {
				continue
			}
			plain[16-padding] ^= 1
		}
		ciphertext := make([]byte, 16)
		cipher.NewCBCEncrypter(block, iv).CryptBlocks(ciphertext, plain)
		if plain, err := decryptCBC(t.Context(), ciphertext, key, iv); plain != nil || !errors.Is(err, ErrDecryptFailed) {
			t.Fatalf("invalid padding %d accepted: %v", padding, err)
		}
		if !bytes.Equal(ciphertext, make([]byte, len(ciphertext))) {
			t.Fatalf("填充错误后未清零缓冲区: %d", padding)
		}
	}
	for _, n := range []int{0, 1, 15, 17, 31} {
		if _, err := decryptCBC(t.Context(), make([]byte, n), key, iv); !errors.Is(err, ErrDecryptFailed) {
			t.Fatalf("invalid ciphertext length %d accepted", n)
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := decryptCBC(ctx, make([]byte, 16), key, iv); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancellation lost: %v", err)
	}
}

// cbcObservedContext 在 Err 检查前观察原地解密进度，避免依赖定时器或调度触发取消。
type cbcObservedContext struct {
	context.Context
	beforeErr func()
}

func (c cbcObservedContext) Err() error {
	c.beforeErr()
	return c.Context.Err()
}

func TestCBCCancellationErasesAllDataAndNeverReturnsPartialPlaintext(t *testing.T) {
	for _, test := range []struct {
		name   string
		size   int
		before bool
	}{
		{"before_first_chunk", 128 << 10, true},
		{"between_chunks", 128 << 10, false},
		{"after_final_chunk", 32, false},
	} {
		t.Run(test.name, func(t *testing.T) {
			key, iv := bytes.Repeat([]byte{0x21}, 16), bytes.Repeat([]byte{0x43}, 16)
			original := bytes.Repeat([]byte{0x65}, test.size)
			data, err := encryptCBC(t.Context(), original, key, iv)
			if err != nil {
				t.Fatal(err)
			}
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			observed := cbcObservedContext{Context: ctx, beforeErr: func() {
				if test.before || bytes.Equal(data[:16], original[:16]) {
					cancel()
				}
			}}
			plain, err := decryptCBC(observed, data, key, iv)
			if !errors.Is(err, context.Canceled) || plain != nil {
				t.Fatalf("取消后返回了明文或丢失取消原因: plain=%d err=%v", len(plain), err)
			}
			if !bytes.Equal(data, make([]byte, len(data))) {
				t.Fatal("取消后仍有已解密明文或未处理密文残留")
			}
			if !bytes.Equal(key, bytes.Repeat([]byte{0x21}, 16)) || !bytes.Equal(iv, bytes.Repeat([]byte{0x43}, 16)) {
				t.Fatal("清零修改了调用方密钥或 IV")
			}
		})
	}
}
