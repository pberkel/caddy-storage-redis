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
	"crypto/tls"
	"errors"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"testing/synctest"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/bsm/redislock"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
	"go.uber.org/zap/zaptest/observer"
)

func TestRedisStorage_AcquireClientLifetime(t *testing.T) {
	rs, ctx := provisionRetainedClientStorage(t)
	first, releaseFirst, err := rs.AcquireClient()
	require.NoError(t, err)
	t.Cleanup(func() { assert.NoError(t, releaseFirst()) })
	second, releaseSecond, err := rs.AcquireClient()
	require.NoError(t, err)
	t.Cleanup(func() { assert.NoError(t, releaseSecond()) })
	assert.Same(t, rs.GetClient(), first)
	assert.Same(t, first, second)
	client := first.(redis.UniversalClient)

	require.NoError(t, rs.Cleanup())
	require.NoError(t, rs.Cleanup())
	require.NoError(t, client.Ping(ctx).Err())
	unavailable, release, err := rs.AcquireClient()
	require.ErrorIs(t, err, ErrClientUnavailable)
	assert.Nil(t, unavailable)
	assert.Nil(t, release)

	require.NoError(t, releaseFirst())
	require.NoError(t, releaseFirst())
	require.NoError(t, client.Ping(ctx).Err())
	require.NoError(t, releaseSecond())
	assert.ErrorIs(t, client.Ping(ctx).Err(), redis.ErrClosed)
}

func TestRedisStorage_ReleaseClientBeforeCleanup(t *testing.T) {
	rs, ctx := provisionRetainedClientStorage(t)
	value, release, err := rs.AcquireClient()
	require.NoError(t, err)
	t.Cleanup(func() { assert.NoError(t, release()) })
	client := value.(redis.UniversalClient)
	require.NoError(t, release())
	require.NoError(t, release())
	require.NoError(t, client.Ping(ctx).Err())
	require.NoError(t, rs.Cleanup())
	assert.ErrorIs(t, client.Ping(ctx).Err(), redis.ErrClosed)
}

func TestRedisStorage_AcquireClientUnprovisioned(t *testing.T) {
	rs := New()
	client, release, err := rs.AcquireClient()
	require.ErrorIs(t, err, ErrClientUnavailable)
	assert.Nil(t, client)
	assert.Nil(t, release)
	assert.NoError(t, rs.Cleanup())
}

func TestRedisStorage_RetainedClientAcrossReplacement(t *testing.T) {
	old, ctx := provisionRetainedClientStorage(t)
	value, release, err := old.AcquireClient()
	require.NoError(t, err)
	t.Cleanup(func() { assert.NoError(t, release()) })
	client := value.(redis.UniversalClient)

	replacement, _ := provisionRetainedClientStorage(t)
	require.NoError(t, old.Cleanup())
	require.NoError(t, client.Ping(ctx).Err())
	require.NoError(t, replacement.client.Ping(ctx).Err())
	require.NoError(t, release())
	assert.ErrorIs(t, client.Ping(ctx).Err(), redis.ErrClosed)
	assert.NoError(t, replacement.client.Ping(ctx).Err())
}

type closeCountingClient struct {
	redis.UniversalClient
	closes atomic.Int32
	err    error
}

func (c *closeCountingClient) Close() error {
	c.closes.Add(1)
	return c.err
}

func TestRedisStorage_ConcurrentClientLifetime(t *testing.T) {
	client := &closeCountingClient{}
	rs := newClientTestStorage(t, client)
	_, releaseLast, err := rs.AcquireClient()
	require.NoError(t, err)
	t.Cleanup(func() { assert.NoError(t, releaseLast()) })

	start := make(chan struct{})
	var wg sync.WaitGroup
	for range 32 {
		wg.Add(2)
		go func() {
			defer wg.Done()
			<-start
			value, release, err := rs.AcquireClient()
			if err != nil {
				assert.ErrorIs(t, err, ErrClientUnavailable)
				return
			}
			assert.Same(t, client, value)
			assert.NoError(t, release())
			assert.NoError(t, release())
		}()
		go func() {
			defer wg.Done()
			<-start
			assert.NoError(t, rs.Cleanup())
		}()
	}
	close(start)
	wg.Wait()
	assert.Zero(t, client.closes.Load())

	for range 32 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			assert.NoError(t, releaseLast())
		}()
	}
	wg.Wait()
	assert.EqualValues(t, 1, client.closes.Load())
}

func TestRedisStorage_ClientCloseError(t *testing.T) {
	for _, retain := range []bool{false, true} {
		client := &closeCountingClient{err: errors.New("close failed")}
		rs := newClientTestStorage(t, client)
		var closeClient func() error
		if retain {
			var err error
			_, closeClient, err = rs.AcquireClient()
			require.NoError(t, err)
			require.NoError(t, rs.Cleanup())
		} else {
			closeClient = rs.Cleanup
		}
		assert.ErrorIs(t, closeClient(), client.err)
		assert.ErrorIs(t, closeClient(), client.err)
		assert.EqualValues(t, 1, client.closes.Load())
	}
}

func provisionRetainedClientStorage(t *testing.T) (*RedisStorage, context.Context) {
	t.Helper()
	mr := miniredis.RunT(t)
	rs := New()
	rs.Address = []string{mr.Addr()}
	rs.ClientShutdownGracePeriod = "0s"
	ctx := context.Background()
	require.NoError(t, rs.finalizeConfiguration(ctx))
	t.Cleanup(func() { _ = rs.Cleanup() })
	return rs, ctx
}

func newClientTestStorage(t *testing.T, client redis.UniversalClient) *RedisStorage {
	t.Helper()
	rs := New()
	rs.poolKeyVal = poolIdentity{ClientType: "simple", Addrs: t.Name()}
	var err error
	rs.client, _, err = defaultPool.acquire(rs.poolKeyVal, nil, func() (redis.UniversalClient, *redislock.Client, error) {
		return client, nil, nil
	}, nil)
	require.NoError(t, err)
	t.Cleanup(func() { _ = rs.Cleanup(); defaultPool.reset() })
	return rs
}

func TestRedisStorage_RetainedClientSharedAcrossReload(t *testing.T) {
	old, ctx := provisionRetainedClientStorage(t)
	replacement := New()
	replacement.Address = old.Address
	replacement.ClientShutdownGracePeriod = "0s"
	require.NoError(t, replacement.finalizeConfiguration(ctx))
	t.Cleanup(func() { _ = replacement.Cleanup() })
	require.Same(t, old.client, replacement.client)

	value, release, err := old.AcquireClient()
	require.NoError(t, err)
	t.Cleanup(func() { _ = release() })
	client := value.(redis.UniversalClient)
	require.NoError(t, old.Cleanup())
	_, _, err = old.AcquireClient()
	require.ErrorIs(t, err, ErrClientUnavailable)
	require.NoError(t, release())
	require.NoError(t, release())
	require.NoError(t, replacement.client.Ping(ctx).Err())

	_, releaseReplacement, err := replacement.AcquireClient()
	require.NoError(t, err)
	t.Cleanup(func() { _ = releaseReplacement() })
	require.NoError(t, replacement.Cleanup())
	require.NoError(t, client.Ping(ctx).Err())
	require.NoError(t, releaseReplacement())
	require.ErrorIs(t, client.Ping(ctx).Err(), redis.ErrClosed)
}

func TestRedisStorage_RetainedClientCredentialRotation(t *testing.T) {
	mr := miniredis.RunT(t)
	mr.RequireUserAuth("first", "old-password")
	mr.RequireUserAuth("second", "new-password")
	ctx := context.Background()
	old := New()
	old.Address = []string{mr.Addr()}
	old.Username, old.Password = "first", "old-password"
	old.ClientShutdownGracePeriod = "0s"
	require.NoError(t, old.finalizeConfiguration(ctx))
	t.Cleanup(func() { _ = old.Cleanup() })
	value, release, err := old.AcquireClient()
	require.NoError(t, err)
	t.Cleanup(func() { _ = release() })
	client := value.(redis.UniversalClient)

	replacement := New()
	replacement.Address = []string{mr.Addr()}
	replacement.Username, replacement.Password = "second", "new-password"
	replacement.ClientShutdownGracePeriod = "0s"
	require.NoError(t, replacement.finalizeConfiguration(ctx))
	t.Cleanup(func() { _ = replacement.Cleanup() })
	require.NotSame(t, client, replacement.client)
	require.Equal(t, "first", client.(*redis.Client).Options().Username)
	require.Equal(t, "second", replacement.client.(*redis.Client).Options().Username)
	require.NoError(t, old.Cleanup())
	require.NoError(t, client.Ping(ctx).Err())
	require.NoError(t, release())
	require.ErrorIs(t, client.Ping(ctx).Err(), redis.ErrClosed)
	require.NoError(t, replacement.client.Ping(ctx).Err())
}

func TestRedisStorage_RetainedClientShutdownDeadline(t *testing.T) {
	for _, tc := range []struct {
		name         string
		releaseAfter time.Duration
	}{
		{"release before deadline", 3 * time.Second},
		{"release after deadline", 11 * time.Second},
	} {
		t.Run(tc.name, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				client := &closeCountingClient{}
				rs := newClientTestStorage(t, client)
				rs.clientShutdownGracePeriodDuration = defaultClientShutdownGracePeriod
				_, release, err := rs.AcquireClient()
				require.NoError(t, err)
				t.Cleanup(func() { _ = release() })
				require.NoError(t, rs.Cleanup())
				time.Sleep(tc.releaseAfter)
				synctest.Wait()
				require.Zero(t, client.closes.Load())
				require.NoError(t, release())
				require.NoError(t, release())
				remaining := defaultClientShutdownGracePeriod - tc.releaseAfter
				if remaining > 0 {
					time.Sleep(remaining - time.Nanosecond)
					synctest.Wait()
					require.Zero(t, client.closes.Load())
					time.Sleep(time.Nanosecond)
				}
				synctest.Wait()
				require.EqualValues(t, 1, client.closes.Load())
			})
		})
	}
}

func TestRedisStorage_DelayedClientCloseError(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		client := &closeCountingClient{err: errors.New("close failed")}
		rs := newClientTestStorage(t, client)
		rs.clientShutdownGracePeriodDuration = defaultClientShutdownGracePeriod
		core, logs := observer.New(zapcore.WarnLevel)
		rs.logger = zap.New(core).Sugar()
		_, release, err := rs.AcquireClient()
		require.NoError(t, err)
		t.Cleanup(func() { _ = release() })
		require.NoError(t, rs.Cleanup())
		require.NoError(t, release())
		time.Sleep(defaultClientShutdownGracePeriod)
		synctest.Wait()
		require.EqualValues(t, 1, client.closes.Load())
		require.Equal(t, 1, logs.FilterMessageSnippet("close failed").Len())
		require.NoError(t, release())
	})
}

func TestRedisStorage_AcquireClientAfterFailedProvision(t *testing.T) {
	mr := miniredis.RunT(t)
	denyRedisPing(mr)
	rs := New()
	rs.Address = []string{mr.Addr()}
	require.ErrorContains(t, rs.finalizeConfiguration(context.Background()), "NOPERM PING denied")
	require.Nil(t, rs.client)
	client, release, err := rs.AcquireClient()
	require.ErrorIs(t, err, ErrClientUnavailable)
	require.Nil(t, client)
	require.Nil(t, release)
	require.NoError(t, rs.Cleanup())
	client, release, err = rs.AcquireClient()
	require.ErrorIs(t, err, ErrClientUnavailable)
	require.Nil(t, client)
	require.Nil(t, release)
}

func TestRedisStorage_ClientLifetimeSharedByValueCopies(t *testing.T) {
	rs, ctx := provisionRetainedClientStorage(t)
	storageCopy := *rs
	_, release, err := storageCopy.AcquireClient()
	require.NoError(t, err)
	t.Cleanup(func() { _ = release() })
	require.NoError(t, rs.Cleanup())
	require.NoError(t, storageCopy.Cleanup())
	_, _, err = storageCopy.AcquireClient()
	require.ErrorIs(t, err, ErrClientUnavailable)
	require.NoError(t, rs.client.Ping(ctx).Err())
	require.NoError(t, release())
	require.ErrorIs(t, rs.client.Ping(ctx).Err(), redis.ErrClosed)
}

func TestRedisStorage_RetainedClientTLSRotation(t *testing.T) {
	certA, pemA := makeRedisTLSCertificate(t, 1)
	certB, pemB := makeRedisTLSCertificate(t, 2)
	var activeCert atomic.Pointer[tls.Certificate]
	activeCert.Store(&certA)
	mr, err := miniredis.RunTLS(&tls.Config{
		GetCertificate: func(*tls.ClientHelloInfo) (*tls.Certificate, error) { return activeCert.Load(), nil },
	})
	require.NoError(t, err)
	defer mr.Close()
	defer defaultPool.reset()
	certPath := filepath.Join(t.TempDir(), "ca.pem")
	require.NoError(t, os.WriteFile(certPath, pemA, 0600))
	newStorage := func() *RedisStorage {
		rs := New()
		rs.Address = []string{mr.Addr()}
		rs.TlsEnabled = true
		rs.TlsServerCertsPath = certPath
		rs.ClientShutdownGracePeriod = "0s"
		require.NoError(t, rs.finalizeConfiguration(context.Background()))
		return rs
	}
	old := newStorage()
	value, release, err := old.AcquireClient()
	require.NoError(t, err)
	client := value.(redis.UniversalClient)
	activeCert.Store(&certB)
	require.NoError(t, os.WriteFile(certPath, pemB, 0600))
	replacement := newStorage()
	require.NotSame(t, client, replacement.client)
	require.False(t, client.(*redis.Client).Options().TLSConfig.RootCAs.Equal(replacement.client.(*redis.Client).Options().TLSConfig.RootCAs))
	require.NoError(t, old.Cleanup())
	require.NoError(t, client.Ping(context.Background()).Err())
	require.NoError(t, release())
	require.ErrorIs(t, client.Ping(context.Background()).Err(), redis.ErrClosed)
	require.NoError(t, replacement.client.Ping(context.Background()).Err())
	require.NoError(t, replacement.Cleanup())
}

func TestRedisStorage_AcquireClientZeroValue(t *testing.T) {
	var rs RedisStorage
	client, release, err := rs.AcquireClient()
	require.ErrorIs(t, err, ErrClientUnavailable)
	require.Nil(t, client)
	require.Nil(t, release)
	require.NoError(t, rs.Cleanup())
}
