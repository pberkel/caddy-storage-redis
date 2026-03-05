package storageredis

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestRedisStorage_EncryptDecrypt(t *testing.T) {

	rs := New()
	rs.EncryptionKey = "1aedfs5kcM8lOZO3BDDMuwC23croDwRr"
	originalValue := []byte("Q2FkZHkgUmVkaXMgU3RvcmFnZQ==")

	encryptedValue, err := rs.encrypt(originalValue)
	assert.NoError(t, err)

	decryptedValue, err := rs.decrypt(encryptedValue)
	assert.NoError(t, err)
	assert.Equal(t, originalValue, decryptedValue)
}

func TestRedisStorage_DecryptShortCiphertextFails(t *testing.T) {
	rs := New()
	rs.EncryptionKey = "1aedfs5kcM8lOZO3BDDMuwC23croDwRr"

	decryptedValue, err := rs.decrypt([]byte("short"))
	assert.Nil(t, decryptedValue)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "Invalid encrypted data")
}

func TestRedisStorage_DecryptWrongKeyFails(t *testing.T) {
	rs := New()
	plaintext := []byte("Q2FkZHkgUmVkaXMgU3RvcmFnZQ==")

	rs.EncryptionKey = "1aedfs5kcM8lOZO3BDDMuwC23croDwRr"
	encryptedValue, err := rs.encrypt(plaintext)
	assert.NoError(t, err)

	rs.EncryptionKey = "abcdefghijklmnopqrstuvwxyz123456"
	decryptedValue, err := rs.decrypt(encryptedValue)
	assert.Nil(t, decryptedValue)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "Decryption failure")
}

func TestRedisStorage_EncryptInvalidKeyFails(t *testing.T) {
	rs := New()
	rs.EncryptionKey = "too-short"

	encryptedValue, err := rs.encrypt([]byte("Q2FkZHkgUmVkaXMgU3RvcmFnZQ=="))
	assert.Nil(t, encryptedValue)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "Unable to create AES cipher")
}
