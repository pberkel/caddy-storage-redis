package storageredis

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"fmt"
	"io"
)

func (rs *RedisStorage) encrypt(bytes []byte) ([]byte, error) {

	c, err := aes.NewCipher([]byte(rs.EncryptionKey))
	if err != nil {
		return nil, fmt.Errorf("Unable to create AES cipher: %v", err)
	}

	gcm, err := cipher.NewGCM(c)
	if err != nil {
		return nil, fmt.Errorf("Unable to create GCM cipher: %v", err)
	}

	nonce := make([]byte, gcm.NonceSize())
	_, err = io.ReadFull(rand.Reader, nonce)
	if err != nil {
		return nil, fmt.Errorf("Unable to generate nonce: %v", err)
	}

	return gcm.Seal(nonce, nonce, bytes, nil), nil
}

func (rs *RedisStorage) decrypt(bytes []byte) ([]byte, error) {

	if len(bytes) < aes.BlockSize {
		return nil, fmt.Errorf("Invalid encrypted data")
	}

	block, err := aes.NewCipher([]byte(rs.EncryptionKey))
	if err != nil {
		return nil, fmt.Errorf("Unable to create AES cipher: %v", err)
	}

	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("Unable to create GCM cipher: %v", err)
	}

	out, err := gcm.Open(nil, bytes[:gcm.NonceSize()], bytes[gcm.NonceSize():], nil)
	if err != nil {
		return nil, fmt.Errorf("Decryption failure: %v", err)
	}

	return out, nil
}
