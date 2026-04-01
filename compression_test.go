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

func TestRedisStorage_FlateCompressDecompress(t *testing.T) {
	rs := New()
	rs.Compression = CompressionFlate
	originalValue := []byte("Q2FkZHkgUmVkaXMgU3RvcmFnZQ==")

	compressedValue, err := rs.compress(originalValue)
	assert.NoError(t, err)

	decompressedValue, err := rs.decompress(compressedValue, storageCompressionFlate)
	assert.NoError(t, err)
	assert.Equal(t, originalValue, decompressedValue)
}

func TestRedisStorage_ZlibCompressDecompress(t *testing.T) {
	rs := New()
	rs.Compression = CompressionZlib
	originalValue := []byte("Q2FkZHkgUmVkaXMgU3RvcmFnZQ==")

	compressedValue, err := rs.compress(originalValue)
	assert.NoError(t, err)

	decompressedValue, err := rs.decompress(compressedValue, storageCompressionZlib)
	assert.NoError(t, err)
	assert.Equal(t, originalValue, decompressedValue)
}

func TestRedisStorage_FlateDecompressExceedsLimit(t *testing.T) {
	rs := New()
	rs.Compression = CompressionFlate
	originalValue := make([]byte, maxDecompressedBytes+1)

	compressedValue, err := rs.compress(originalValue)
	assert.NoError(t, err)

	decompressedValue, err := rs.decompress(compressedValue, storageCompressionFlate)
	assert.Nil(t, decompressedValue)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "decompressed value exceeds limit")
}

func TestRedisStorage_ZlibDecompressExceedsLimit(t *testing.T) {
	rs := New()
	rs.Compression = CompressionZlib
	originalValue := make([]byte, maxDecompressedBytes+1)

	compressedValue, err := rs.compress(originalValue)
	assert.NoError(t, err)

	decompressedValue, err := rs.decompress(compressedValue, storageCompressionZlib)
	assert.Nil(t, decompressedValue)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "decompressed value exceeds limit")
}
