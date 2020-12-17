package sym

import (
	"bytes"
	"crypto/cipher"
	"crypto/des"
	"crypto/rand"
	"errors"
)

// TripleDES a 3DES instance is a tool to encrypt and decrypt
// Very not recommended to use 3des!!! It's slow and unsafe
type ThirdDESKey struct {
	key []byte
}

func (ea *ThirdDESKey) Bytes() ([]byte, error) {
	r := make([]byte, len(ea.key))
	copy(r, ea.key)

	return r, nil
}

// Encrypt encrypt
func (ea *ThirdDESKey) Encrypt(plain []byte) (cipher []byte, err error) {
	return TripleDesEnc(ea.key, plain)
}

// Decrypt decrypt
func (ea *ThirdDESKey) Decrypt(cipherTex []byte) (plaintext []byte, err error) {
	return TripleDesDec(ea.key, cipherTex)
}

// TripleDesEnc encryption algorithm implements
func TripleDesEnc(key, src []byte) ([]byte, error) {
	if len(key) < 24 {
		return nil, errors.New("the secret len is less than 24")
	}
	block, err := des.NewTripleDESCipher(key[:24])
	if err != nil {
		return nil, err
	}
	msg := PKCS5Padding(src, block.BlockSize())
	iv := make([]byte, block.BlockSize())
	if _, err := rand.Read(iv); err != nil {
		return nil, err
	}
	blockMode := cipher.NewCBCEncrypter(block, iv)
	crypted := make([]byte, len(msg)+len(iv))
	blockMode.CryptBlocks(crypted[block.BlockSize():], msg)
	copy(crypted[0:block.BlockSize()], iv)
	return crypted, nil
}

// TripleDesDec decryption algorithm implements
func TripleDesDec(key, src []byte) ([]byte, error) {
	// log.Criticalf("to descrypt msg is : %s",common.ToHex(src))
	if len(key) < 24 {
		return nil, errors.New("the secret len is less than 24")
	}
	block, err := des.NewTripleDESCipher(key[:24])
	if err != nil {
		return nil, err
	}
	blockMode := cipher.NewCBCDecrypter(block, src[:block.BlockSize()])
	// log.Criticalf("dec block size:%d , src len %d, %d",blockMode.BlockSize(),len(src),len(src)%block.BlockSize())
	origData := make([]byte, len(src)-block.BlockSize())
	blockMode.CryptBlocks(origData, src[block.BlockSize():])
	origData, err = PKCS5UnPadding(origData)
	if err != nil {
		return nil, err
	}
	return origData, nil
}

// PKCS5Padding padding with pkcs5
func PKCS5Padding(ciphertext []byte, blockSize int) []byte {
	padding := blockSize - len(ciphertext)%blockSize
	padtext := bytes.Repeat([]byte{byte(padding)}, padding)
	return append(ciphertext, padtext...)
}

// PKCS5UnPadding unpadding with pkcs5
func PKCS5UnPadding(origData []byte) ([]byte, error) {
	length := len(origData)
	// 去掉最后一个字节 unpadding 次
	unpadding := int(origData[length-1])
	if unpadding > length {
		return nil, errors.New("decrypt failed,please check it")
	}
	return origData[:(length - unpadding)], nil
}
