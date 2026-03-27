package storageredis

import (
	"context"
	"net"
	"strings"
	"testing"

	"github.com/alicebob/miniredis/v2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

func newFinalizeTestStorage(t *testing.T) (*RedisStorage, *miniredis.Miniredis) {
	t.Helper()

	mr, err := miniredis.Run()
	if err != nil {
		// Some sandboxes disallow opening listening sockets used by miniredis.
		if strings.Contains(err.Error(), "operation not permitted") {
			t.Skipf("miniredis unavailable in this environment: %v", err)
		}
		require.NoError(t, err)
	}
	t.Cleanup(mr.Close)

	rs := New()
	logger, _ := zap.NewProduction()
	rs.logger = logger.Sugar()

	return rs, mr
}

func TestFinalizeConfiguration_FailoverRequiresMasterName(t *testing.T) {
	t.Parallel()

	t.Run("failover without master_name rejected", func(t *testing.T) {
		rs := New()
		rs.ClientType = "failover"
		rs.Address = []string{"127.0.0.1:26379"}

		err := rs.finalizeConfiguration(context.Background())
		require.Error(t, err)
		assert.Contains(t, err.Error(), "master_name")
	})

	t.Run("failover with master_name accepted", func(t *testing.T) {
		rs, mr := newFinalizeTestStorage(t)
		rs.ClientType = "failover"
		rs.MasterName = "mymaster"
		rs.Address = []string{mr.Addr()}

		// miniredis does not implement Sentinel; we only verify that
		// the master_name check itself does not return an error.
		err := rs.finalizeConfiguration(context.Background())
		if err != nil {
			assert.NotContains(t, err.Error(), "master_name")
		}
	})
}

func TestFinalizeConfiguration_CompressionPlaceholder(t *testing.T) {
	t.Parallel()

	t.Run("flate placeholder resolved", func(t *testing.T) {
		rs, mr := newFinalizeTestStorage(t)
		rs.Address = []string{mr.Addr()}
		// Simulate a placeholder that has already been resolved to "flate"
		rs.Compression = CompressionMode("flate")

		err := rs.finalizeConfiguration(context.Background())
		require.NoError(t, err)
		assert.Equal(t, CompressionFlate, rs.Compression)
	})

	t.Run("zlib placeholder resolved", func(t *testing.T) {
		rs, mr := newFinalizeTestStorage(t)
		rs.Address = []string{mr.Addr()}
		rs.Compression = CompressionMode("zlib")

		err := rs.finalizeConfiguration(context.Background())
		require.NoError(t, err)
		assert.Equal(t, CompressionZlib, rs.Compression)
	})

	t.Run("false string normalised to none", func(t *testing.T) {
		rs, mr := newFinalizeTestStorage(t)
		rs.Address = []string{mr.Addr()}
		rs.Compression = CompressionMode("false")

		err := rs.finalizeConfiguration(context.Background())
		require.NoError(t, err)
		assert.Equal(t, CompressionNone, rs.Compression)
	})

	t.Run("invalid compression value rejected", func(t *testing.T) {
		rs := New()
		rs.Compression = CompressionMode("gzip")

		err := rs.finalizeConfiguration(context.Background())
		require.Error(t, err)
		assert.Contains(t, err.Error(), "invalid compression value")
	})
}

func TestFinalizeConfiguration_DBPlaceholder(t *testing.T) {
	t.Parallel()

	t.Run("valid db value accepted", func(t *testing.T) {
		rs, mr := newFinalizeTestStorage(t)
		rs.Address = []string{mr.Addr()}
		rs.DB = DBIndex("3")

		err := rs.finalizeConfiguration(context.Background())
		require.NoError(t, err)
		assert.Equal(t, DBIndex("3"), rs.DB)
	})

	t.Run("negative db rejected", func(t *testing.T) {
		rs := New()
		rs.DB = DBIndex("-1")

		err := rs.finalizeConfiguration(context.Background())
		require.Error(t, err)
		assert.Contains(t, err.Error(), "invalid db value")
	})

	t.Run("non-numeric db rejected", func(t *testing.T) {
		rs := New()
		rs.DB = DBIndex("abc")

		err := rs.finalizeConfiguration(context.Background())
		require.Error(t, err)
		assert.Contains(t, err.Error(), "invalid db value")
	})
}

func TestFinalizeConfiguration_KeyPrefixNormalization(t *testing.T) {
	t.Parallel()

	t.Run("normalizes slashes", func(t *testing.T) {
		rs, mr := newFinalizeTestStorage(t)
		rs.Address = []string{mr.Addr()}
		rs.KeyPrefix = "/caddy/"

		err := rs.finalizeConfiguration(context.Background())
		require.NoError(t, err)
		assert.Equal(t, "caddy", rs.KeyPrefix)
	})

	t.Run("allows empty", func(t *testing.T) {
		rs, mr := newFinalizeTestStorage(t)
		rs.Address = []string{mr.Addr()}
		rs.KeyPrefix = ""

		err := rs.finalizeConfiguration(context.Background())
		require.NoError(t, err)
		assert.Equal(t, "", rs.KeyPrefix)
	})

	t.Run("rejects traversal segment", func(t *testing.T) {
		rs := New()
		rs.KeyPrefix = "a/../b"

		err := rs.finalizeConfiguration(context.Background())
		require.Error(t, err)
		assert.Contains(t, err.Error(), "invalid key_prefix segment")
	})
}

func TestFinalizeConfiguration_EncryptionKeyBoundaries(t *testing.T) {
	t.Parallel()

	t.Run("31 chars rejected", func(t *testing.T) {
		rs := New()
		rs.EncryptionKey = "1234567890123456789012345678901"

		err := rs.finalizeConfiguration(context.Background())
		require.Error(t, err)
		assert.Contains(t, err.Error(), "invalid length for 'encryption_key'")
	})

	t.Run("32 chars accepted", func(t *testing.T) {
		rs, mr := newFinalizeTestStorage(t)
		rs.Address = []string{mr.Addr()}
		originalKey := "12345678901234567890123456789012"
		rs.EncryptionKey = originalKey

		err := rs.finalizeConfiguration(context.Background())
		require.NoError(t, err)
		assert.Equal(t, originalKey, rs.EncryptionKey)
	})

	t.Run("33 chars truncated", func(t *testing.T) {
		rs, mr := newFinalizeTestStorage(t)
		rs.Address = []string{mr.Addr()}
		rs.EncryptionKey = "123456789012345678901234567890123"

		err := rs.finalizeConfiguration(context.Background())
		require.NoError(t, err)
		assert.Equal(t, "12345678901234567890123456789012", rs.EncryptionKey)
	})
}

func TestFinalizeConfiguration_TimeoutValidation(t *testing.T) {
	t.Parallel()

	t.Run("valid positive timeout", func(t *testing.T) {
		rs, mr := newFinalizeTestStorage(t)
		rs.Address = []string{mr.Addr()}
		rs.Timeout = "5"

		err := rs.finalizeConfiguration(context.Background())
		require.NoError(t, err)
		assert.Equal(t, "5", rs.Timeout)
	})

	t.Run("negative timeout rejected", func(t *testing.T) {
		rs := New()
		rs.Timeout = "-1"

		err := rs.finalizeConfiguration(context.Background())
		require.Error(t, err)
		assert.Contains(t, err.Error(), "invalid timeout value")
	})

	t.Run("non numeric timeout rejected", func(t *testing.T) {
		rs := New()
		rs.Timeout = "abc"

		err := rs.finalizeConfiguration(context.Background())
		require.Error(t, err)
		assert.Contains(t, err.Error(), "invalid timeout value")
	})
}

func TestFinalizeConfiguration_AddressHostPortValidation(t *testing.T) {
	t.Parallel()

	t.Run("invalid address rejected", func(t *testing.T) {
		rs := New()
		rs.Address = []string{"invalid-address"}

		err := rs.finalizeConfiguration(context.Background())
		require.Error(t, err)
		assert.Contains(t, err.Error(), "invalid address")
	})

	t.Run("invalid host rejected", func(t *testing.T) {
		rs := New()
		rs.Address = nil
		rs.Host = []string{"invalid host value"}
		rs.Port = []string{"6379"}

		err := rs.finalizeConfiguration(context.Background())
		require.Error(t, err)
		assert.Contains(t, err.Error(), "invalid host value")
	})

	t.Run("invalid port rejected", func(t *testing.T) {
		rs := New()
		rs.Address = nil
		rs.Host = []string{"127.0.0.1"}
		rs.Port = []string{"bad-port"}

		err := rs.finalizeConfiguration(context.Background())
		require.Error(t, err)
		assert.Contains(t, err.Error(), "invalid port value")
	})

	t.Run("host and port build address", func(t *testing.T) {
		rs, mr := newFinalizeTestStorage(t)
		host, port, err := net.SplitHostPort(mr.Addr())
		require.NoError(t, err)

		rs.Address = nil
		rs.Host = []string{host}
		rs.Port = []string{port}

		err = rs.finalizeConfiguration(context.Background())
		require.NoError(t, err)
		assert.Equal(t, []string{mr.Addr()}, rs.Address)
		assert.Empty(t, rs.Host)
		assert.Empty(t, rs.Port)
	})
}
