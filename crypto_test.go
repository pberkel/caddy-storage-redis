// Copyright 2024 Pieter Berkel
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

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

	// 5 bytes: well below the nonce+tag minimum
	decryptedValue, err := rs.decrypt([]byte("short"))
	assert.Nil(t, decryptedValue)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "Invalid encrypted data")

	// 27 bytes: one below the minimum (nonce=12 + tag=16 = 28)
	decryptedValue, err = rs.decrypt(make([]byte, 27))
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
