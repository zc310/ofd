package crypt

import (
	"bytes"
	"context"
	"crypto"
	"crypto/cipher"
	"crypto/ecdsa"
	"crypto/rand"
	"crypto/subtle"
	"encoding/asn1"
	"errors"
	"fmt"
	"math/big"

	"github.com/emmansun/gmsm/sm2"
	"github.com/emmansun/gmsm/sm3"
	"github.com/emmansun/gmsm/sm4"
	"github.com/emmansun/gmsm/smx509"
)

// CBC 解密原地修改缓冲区，填充检查不验证明文完整性。
// 失败时清零整个输入缓冲且不返回部分明文；成功后由调用方负责清零。
func decryptCBC(ctx context.Context, data, key, iv []byte) (plain []byte, err error) {
	defer func() {
		if err != nil {
			clear(data)
			plain = nil
		}
	}()
	if len(data) == 0 || len(data)%sm4.BlockSize != 0 || len(key) != sm4.BlockSize || len(iv) != sm4.BlockSize {
		return nil, ErrDecryptFailed
	}
	block, err := sm4.NewCipher(key)
	if err != nil {
		return nil, ErrDecryptFailed
	}
	cbc := cipher.NewCBCDecrypter(block, iv)
	// 分块处理以限制取消延迟，同时在分块之间保留 CBC 状态。
	for offset := 0; offset < len(data); {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		end := min(offset+64*1024, len(data))
		cbc.CryptBlocks(data[offset:end], data[offset:end])
		offset = end
	}
	padding := int(data[len(data)-1])
	valid := subtle.ConstantTimeLessOrEq(1, padding) & subtle.ConstantTimeLessOrEq(padding, sm4.BlockSize)
	for i := 1; i <= sm4.BlockSize; i++ {
		match := subtle.ConstantTimeByteEq(data[len(data)-i], byte(padding))
		valid &= (1 - subtle.ConstantTimeLessOrEq(i, padding)) | match
	}
	if valid != 1 {
		return nil, ErrDecryptFailed
	}
	return data[:len(data)-padding], ctx.Err()
}

// passwordKEK 按 GB/T 32918.3 §5.4.3 使用 SM3 派生 128 位结果。
// 输入为原始 UTF-8 口令，不添加盐或终止符，也不进行归一化。
func passwordKEK(password []byte) []byte { return sm3.Kdf(password, 16) }

func unwrapPassword(ctx context.Context, user userXML, kek []byte) ([]byte, []byte, error) {
	wk, err := decodeBase64(user.WrappedKey)
	if err != nil || len(wk) != 32 {
		return nil, nil, ErrDecryptFailed
	}
	defer clear(wk)
	iv, err := userIV(user)
	if err != nil {
		return nil, nil, err
	}
	plain, err := decryptCBC(ctx, wk, kek, iv)
	if err != nil || len(plain) != 16 {
		clear(iv)
		if err != nil {
			return nil, nil, err
		}
		return nil, nil, ErrDecryptFailed
	}
	return bytes.Clone(plain), iv, nil
}

func userIV(user userXML) ([]byte, error) {
	if user.IV == nil {
		return make([]byte, 16), nil
	}
	iv, err := decodeBase64(*user.IV)
	if err != nil || len(iv) != 16 {
		return nil, ErrDecryptFailed
	}
	return iv, nil
}

type sm2Cipher struct {
	X, Y       *big.Int
	Hash       []byte
	Ciphertext []byte
}

// 解密器始终接收规范的 ASN.1 SM2Cipher。仅在 compat 模式下接受包含非压缩点的
// 裸 C1C3C2 编码，并在调用硬件解密器前转换格式。
func sm2WrappedKey(data []byte, mode Mode) ([]byte, bool, error) {
	if len(data) == 113 && data[0] == 4 {
		if mode != ModeCompat {
			return nil, false, fmt.Errorf("%w: 裸 C1C3C2 包装密钥需使用 compat 模式", ErrUnsupported)
		}
		der, err := sm2.PlainCiphertext2ASN1(data, sm2.C1C3C2)
		if err != nil {
			return nil, false, ErrDecryptFailed
		}
		data = der
		if _, _, err := sm2WrappedKey(data, ModeStrict); err != nil {
			return nil, false, err
		}
		return data, true, nil
	}
	var value sm2Cipher
	rest, err := asn1.Unmarshal(data, &value)
	if err != nil || len(rest) != 0 || value.X == nil || value.Y == nil || len(value.Hash) != 32 || len(value.Ciphertext) != 16 {
		return nil, false, ErrDecryptFailed
	}
	coordinateSize := (sm2.P256().Params().BitSize + 7) / 8
	if value.X.Sign() < 0 || value.Y.Sign() < 0 || value.X.BitLen() > coordinateSize*8 || value.Y.BitLen() > coordinateSize*8 {
		return nil, false, ErrDecryptFailed
	}
	point := make([]byte, 1+2*coordinateSize)
	point[0] = 4
	value.X.FillBytes(point[1 : 1+coordinateSize])
	value.Y.FillBytes(point[1+coordinateSize:])
	if _, err := sm2.NewPublicKey(point); err != nil {
		return nil, false, ErrDecryptFailed
	}
	canonical, err := asn1.Marshal(value)
	if err != nil || !bytes.Equal(canonical, data) {
		return nil, false, ErrDecryptFailed
	}
	return data, false, nil
}

func sm2Public(key crypto.Decrypter) (*ecdsa.PublicKey, error) {
	if private, ok := key.(*sm2.PrivateKey); ok && private == nil {
		return nil, fmt.Errorf("SM2 私钥为空")
	}
	pub, ok := key.Public().(*ecdsa.PublicKey)
	if !ok || pub == nil || !sm2.IsSM2PublicKey(pub) {
		return nil, fmt.Errorf("解密器应提供有效的 SM2 公钥")
	}
	der, err := smx509.MarshalPKIXPublicKey(pub)
	if err != nil {
		return nil, fmt.Errorf("解密器应提供有效的 SM2 公钥")
	}
	parsed, err := smx509.ParsePKIXPublicKey(der)
	if err != nil {
		return nil, fmt.Errorf("解密器应提供有效的 SM2 公钥")
	}
	validated, ok := parsed.(*ecdsa.PublicKey)
	if !ok || !sm2.IsSM2PublicKey(validated) {
		return nil, fmt.Errorf("解密器应提供有效的 SM2 公钥")
	}
	return validated, nil
}

func unwrapCertificate(ctx context.Context, user userXML, key crypto.Decrypter, pub *ecdsa.PublicKey, mode Mode) ([]byte, []byte, bool, error) {
	if err := ctx.Err(); err != nil {
		return nil, nil, false, err
	}
	der, err := decodeBase64(user.Certificate)
	if err != nil {
		return nil, nil, false, ErrDecryptFailed
	}
	cert, err := smx509.ParseCertificate(der)
	if err != nil {
		return nil, nil, false, ErrDecryptFailed
	}
	certPub, ok := cert.PublicKey.(*ecdsa.PublicKey)
	if !ok || !pub.Equal(certPub) {
		return nil, nil, false, ErrDecryptFailed
	}
	wk, err := decodeBase64(user.WrappedKey)
	if err != nil {
		return nil, nil, false, ErrDecryptFailed
	}
	wrapped, raw, err := sm2WrappedKey(wk, mode)
	if err != nil {
		return nil, nil, false, err
	}
	fek, err := key.Decrypt(rand.Reader, wrapped, sm2.ASN1DecrypterOpts)
	if ctx.Err() != nil {
		clear(fek)
		return nil, nil, false, ctx.Err()
	}
	if err != nil {
		clear(fek)
		if !errors.Is(err, sm2.ErrDecryption) {
			return nil, nil, false, fmt.Errorf("SM2 密钥服务失败: %w", err)
		}
		return nil, nil, false, ErrDecryptFailed
	}
	if len(fek) != 16 {
		clear(fek)
		return nil, nil, false, ErrDecryptFailed
	}
	iv, err := userIV(user)
	if err != nil {
		clear(fek)
		return nil, nil, false, err
	}
	return fek, iv, raw, nil
}
