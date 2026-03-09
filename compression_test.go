package storageredis

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestRedisStorage_ComressUncompress(t *testing.T) {

	rs := New()
	originalValue := []byte("Q2FkZHkgUmVkaXMgU3RvcmFnZQ==")

	compressedValue, err := rs.compress(originalValue)
	assert.NoError(t, err)

	uncompressedValue, err := rs.uncompress(compressedValue)
	assert.NoError(t, err)
	assert.Equal(t, originalValue, uncompressedValue)
}

func TestRedisStorage_UncompressExceedsLimit(t *testing.T) {
	rs := New()
	originalValue := make([]byte, maxDecompressedBytes+1)

	compressedValue, err := rs.compress(originalValue)
	assert.NoError(t, err)

	uncompressedValue, err := rs.uncompress(compressedValue)
	assert.Nil(t, uncompressedValue)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "decompressed value exceeds limit")
}
