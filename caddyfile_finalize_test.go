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
	"context"
	"net"
	"strings"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/caddyserver/caddy/v2/caddyconfig/caddyfile"
	"github.com/redis/go-redis/v9"
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

func TestFinalizeConfiguration_ClientName(t *testing.T) {
	t.Setenv("REDIS_CLIENT_NAME", "caddy-test")

	rs, mr := newFinalizeTestStorage(t)
	rs.Address = []string{mr.Addr()}
	rs.ClientShutdownGracePeriod = "0s"
	d := caddyfile.NewTestDispenser(`redis {
		client_name {env.REDIS_CLIENT_NAME}
	}`)
	require.NoError(t, rs.UnmarshalCaddyfile(d))

	err := rs.finalizeConfiguration(context.Background())
	require.NoError(t, err)
	t.Cleanup(func() { _ = rs.Cleanup() })

	assert.Equal(t, "caddy-test", rs.ClientName)
	name, err := rs.client.ClientGetName(context.Background()).Result()
	require.NoError(t, err)
	assert.Equal(t, "caddy-test", name)
}

func TestFinalizeConfiguration_SkipConnectionCheck(t *testing.T) {
	t.Parallel()

	// A listener that drops every connection: the port stays reserved for the
	// test, but no Redis command ever succeeds on it.
	unreachableAddr := func(t *testing.T) string {
		t.Helper()
		l, err := net.Listen("tcp", "127.0.0.1:0")
		require.NoError(t, err)
		t.Cleanup(func() { _ = l.Close() })
		go func() {
			for {
				conn, err := l.Accept()
				if err != nil {
					return
				}
				_ = conn.Close()
			}
		}()
		return l.Addr().String()
	}
	newStorage := func(addr string) *RedisStorage {
		rs := New()
		logger, _ := zap.NewProduction()
		rs.logger = logger.Sugar()
		rs.Address = []string{addr}
		rs.Timeout = "1"
		rs.ClientShutdownGracePeriod = "0s"
		return rs
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	t.Cleanup(cancel)

	t.Run("unreachable server rejected by default", func(t *testing.T) {
		rs := newStorage(unreachableAddr(t))

		err := rs.finalizeConfiguration(ctx)
		require.Error(t, err)
	})

	t.Run("unreachable server accepted when skipped, failing on first use", func(t *testing.T) {
		rs := newStorage(unreachableAddr(t))
		rs.SkipConnectionCheck = true

		err := rs.finalizeConfiguration(ctx)
		require.NoError(t, err)
		t.Cleanup(func() { _ = rs.Cleanup() })

		err = rs.Store(ctx, "certificates/example.com/example.com.crt", []byte("value"))
		require.Error(t, err)
	})

	t.Run("an unprobed pooled client is not reused by an instance that asked for the check", func(t *testing.T) {
		addr := unreachableAddr(t)
		skipped := newStorage(addr)
		skipped.SkipConnectionCheck = true
		require.NoError(t, skipped.finalizeConfiguration(ctx))
		t.Cleanup(func() { _ = skipped.Cleanup() })

		strict := newStorage(addr)
		err := strict.finalizeConfiguration(ctx)
		require.Error(t, err)
	})

	t.Run("caddyfile unmarshals skip_connection_check", func(t *testing.T) {
		d := caddyfile.NewTestDispenser(`
			redis {
				address 127.0.0.1:6379
				skip_connection_check true
			}
		`)
		rs := New()
		require.NoError(t, rs.UnmarshalCaddyfile(d))
		assert.True(t, rs.SkipConnectionCheck)

		d = caddyfile.NewTestDispenser(`
			redis {
				skip_connection_check maybe
			}
		`)
		err := New().UnmarshalCaddyfile(d)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "skip_connection_check")
	})
}

func TestFinalizeConfiguration_ClientNamePooling(t *testing.T) {
	t.Setenv("REDIS_CLIENT_NAME", "caddy-first")
	_, mr := newFinalizeTestStorage(t)
	ctx := context.Background()
	newStorage := func(name string) *RedisStorage {
		t.Helper()
		rs := New()
		rs.logger = zap.NewNop().Sugar()
		rs.Address = []string{mr.Addr()}
		rs.ClientName = name
		rs.ClientShutdownGracePeriod = "0s"
		require.NoError(t, rs.finalizeConfiguration(ctx))
		t.Cleanup(func() { require.NoError(t, rs.Cleanup()) })
		return rs
	}

	first := newStorage("{env.REDIS_CLIENT_NAME}")
	sameName := newStorage("caddy-first")
	otherName := newStorage("caddy-second")
	unnamed := newStorage("")

	assert.Same(t, first.client, sameName.client)
	assert.NotSame(t, first.client, otherName.client)
	assert.NotSame(t, first.client, unnamed.client)
	assert.NotSame(t, otherName.client, unnamed.client)
	for _, rs := range []*RedisStorage{first, sameName, otherName, unnamed} {
		name, err := rs.client.ClientGetName(ctx).Result()
		if rs.ClientName == "" {
			require.ErrorIs(t, err, redis.Nil)
		} else {
			require.NoError(t, err)
		}
		assert.Equal(t, rs.ClientName, name)
	}

	// Cleaning up one owner must not close another owner's shared client.
	require.NoError(t, first.Cleanup())
	name, err := sameName.client.ClientGetName(ctx).Result()
	require.NoError(t, err)
	assert.Equal(t, "caddy-first", name)
}
