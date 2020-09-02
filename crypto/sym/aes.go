package sym

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
)

// AES a AES instance is a tool to encrypt and decrypt
type AESKey struct {
	key []byte
}

// Bytes return bytes
func (ek *AESKey) Bytes() ([]byte, error) {
	r := make([]byte, len(ek.key))
	copy(r, ek.key)
	return r, nil
}

// Encrypt encrypt
func (ek *AESKey) Encrypt(plain []byte) ([]byte, error) {
	block, err := aes.NewCipher(ek.key)
	if err != nil {
		return nil, err
	}
	msg := PKCS5Padding(plain, block.BlockSize())
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

// Decrypt decrypt
func (ek *AESKey) Decrypt(crypted []byte) ([]byte, error) {
	block, err := aes.NewCipher(ek.key)
	if err != nil {
		return nil, err
	}

	blockMode := cipher.NewCBCDecrypter(block, crypted[:block.BlockSize()])
	orig := make([]byte, len(crypted)-block.BlockSize())
	blockMode.CryptBlocks(orig, crypted[block.BlockSize():])

	orig, err = PKCS5UnPadding(orig)
	if err != nil {
		return nil, err
	}
	return orig, nil
}
