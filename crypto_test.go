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
