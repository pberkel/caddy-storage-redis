package storageredis

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestRedisStorage_PrefixKey(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		prefix    string
		key       string
		expectKey string
	}{
		{
			name:      "default prefix and simple key",
			prefix:    "caddy",
			key:       "certificates/example.com.crt",
			expectKey: "caddy/certificates/example.com.crt",
		},
		{
			name:      "empty key returns prefix",
			prefix:    "caddy",
			key:       "",
			expectKey: "caddy",
		},
		{
			name:      "empty prefix returns key",
			prefix:    "",
			key:       "certificates/example.com.crt",
			expectKey: "certificates/example.com.crt",
		},
		{
			name:      "both empty returns empty string",
			prefix:    "",
			key:       "",
			expectKey: "",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			rs := New()
			rs.KeyPrefix = tc.prefix
			assert.Equal(t, tc.expectKey, rs.prefixKey(tc.key))
		})
	}
}

func TestRedisStorage_PrefixLock(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		prefix    string
		lockName  string
		expectKey string
	}{
		{
			name:      "default prefix lock key",
			prefix:    "caddy",
			lockName:  "issue_cert_example.com",
			expectKey: "caddy/locks/issue_cert_example.com",
		},
		{
			name:      "nested lock name",
			prefix:    "caddy",
			lockName:  "group/issue_cert_example.com",
			expectKey: "caddy/locks/group/issue_cert_example.com",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			rs := New()
			rs.KeyPrefix = tc.prefix
			assert.Equal(t, tc.expectKey, rs.prefixLock(tc.lockName))
		})
	}
}

func TestRedisStorage_SplitDirectoryKey(t *testing.T) {
	t.Parallel()

	rs := New()

	t.Run("file key", func(t *testing.T) {
		dir, base := rs.splitDirectoryKey("caddy/certificates/example.com.crt", false)
		assert.Equal(t, "caddy/certificates", dir)
		assert.Equal(t, "example.com.crt", base)
	})

	t.Run("directory key", func(t *testing.T) {
		dir, base := rs.splitDirectoryKey("caddy/certificates", true)
		assert.Equal(t, "caddy", dir)
		assert.Equal(t, "certificates/", base)
	})
}

func TestRedisStorage_TrimKey(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		prefix    string
		inputKey  string
		expectKey string
	}{
		{
			name:      "trim configured prefix",
			prefix:    "caddy",
			inputKey:  "caddy/certificates/example.com.crt",
			expectKey: "certificates/example.com.crt",
		},
		{
			name:      "trimKey only removes one leading slash",
			prefix:    "caddy",
			inputKey:  "caddy//certificates/example.com.crt",
			expectKey: "/certificates/example.com.crt",
		},
		{
			name:      "empty prefix trims only leading slash",
			prefix:    "",
			inputKey:  "/certificates/example.com.crt",
			expectKey: "certificates/example.com.crt",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			rs := New()
			rs.KeyPrefix = tc.prefix
			assert.Equal(t, tc.expectKey, rs.trimKey(tc.inputKey))
		})
	}
}
