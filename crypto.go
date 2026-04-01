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

	block, err := aes.NewCipher([]byte(rs.EncryptionKey))
	if err != nil {
		return nil, fmt.Errorf("Unable to create AES cipher: %v", err)
	}

	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("Unable to create GCM cipher: %v", err)
	}

	if len(bytes) < gcm.NonceSize()+gcm.Overhead() {
		return nil, fmt.Errorf("Invalid encrypted data")
	}

	out, err := gcm.Open(nil, bytes[:gcm.NonceSize()], bytes[gcm.NonceSize():], nil)
	if err != nil {
		return nil, fmt.Errorf("Decryption failure: %v", err)
	}

	return out, nil
}
