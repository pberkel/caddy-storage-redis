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
