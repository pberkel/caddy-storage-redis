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
	"sync/atomic"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/bsm/redislock"
	"github.com/caddyserver/caddy/v2/caddyconfig/caddyfile"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

func TestClientPool_String(t *testing.T) {
	key := poolIdentity{ClientType: "simple", Addrs: "test-pool-key", DB: "database"}
	assert.Equal(t, key.String(), "simple|test-pool-key|database")
}

func TestClientPool_ReferenceCounting(t *testing.T) {
	mr, err := miniredis.Run()
	require.NoError(t, err)
	defer mr.Close()

	pool := newRedisClientPool()
	defer pool.reset()

	var factoryCalls int32
	factory := func() (redis.UniversalClient, *redislock.Client, error) {
		atomic.AddInt32(&factoryCalls, 1)
		c := redis.NewClient(&redis.Options{Addr: mr.Addr()})
		return c, redislock.New(c), nil
	}

	key := poolIdentity{ClientType: "simple", Addrs: "test-pool-key"}

	// First acquire: factory is called, refCount becomes 1
	c1, _, err := pool.acquire(key, nil, factory)
	require.NoError(t, err)
	assert.Equal(t, int32(1), atomic.LoadInt32(&factoryCalls))
	assert.Equal(t, 1, pool.getRefCount(key))

	// Second acquire with same key: factory not called, refCount becomes 2
	c2, _, err := pool.acquire(key, nil, factory)
	require.NoError(t, err)
	assert.Equal(t, int32(1), atomic.LoadInt32(&factoryCalls))
	assert.Equal(t, 2, pool.getRefCount(key))
	assert.Same(t, c1, c2)

	// First release: refCount drops to 1, client not closed
	pool.release(key, 0, nil)
	assert.Equal(t, 1, pool.getRefCount(key))
	assert.NoError(t, c1.Ping(context.Background()).Err())

	// Second release with gracePeriod 0: client closed immediately, entry deleted
	pool.release(key, 0, nil)
	assert.Equal(t, 0, pool.getRefCount(key))
	assert.Equal(t, 0, pool.len())
	assert.Error(t, c1.Ping(context.Background()).Err())
}

func TestClientPool_DelayedShutdown(t *testing.T) {
	mr, err := miniredis.Run()
	require.NoError(t, err)
	defer mr.Close()

	pool := newRedisClientPool()
	defer pool.reset()

	factory := func() (redis.UniversalClient, *redislock.Client, error) {
		c := redis.NewClient(&redis.Options{Addr: mr.Addr()})
		return c, redislock.New(c), nil
	}

	key := poolIdentity{ClientType: "simple", Addrs: "test-linger-key"}
	client, _, err := pool.acquire(key, nil, factory)
	require.NoError(t, err)

	gracePeriod := 80 * time.Millisecond
	pool.release(key, gracePeriod, nil)

	// Immediately after release: refCount is 0, but timer is active and client still works
	assert.Equal(t, 0, pool.getRefCount(key))
	assert.True(t, pool.hasLingerTimer(key))
	assert.NoError(t, client.Ping(context.Background()).Err())

	// Wait for grace period to expire plus buffer
	time.Sleep(gracePeriod + 50*time.Millisecond)

	// Entry must now be removed from pool and client closed
	assert.Equal(t, 0, pool.len())
	assert.Error(t, client.Ping(context.Background()).Err())
}

func TestClientPool_ReacquireDuringGracePeriod(t *testing.T) {
	mr, err := miniredis.Run()
	require.NoError(t, err)
	defer mr.Close()

	pool := newRedisClientPool()
	defer pool.reset()

	var factoryCalls int32
	factory := func() (redis.UniversalClient, *redislock.Client, error) {
		atomic.AddInt32(&factoryCalls, 1)
		c := redis.NewClient(&redis.Options{Addr: mr.Addr()})
		return c, redislock.New(c), nil
	}

	key := poolIdentity{ClientType: "simple", Addrs: "test-reacquire-key"}
	c1, _, err := pool.acquire(key, nil, factory)
	require.NoError(t, err)
	assert.Equal(t, int32(1), atomic.LoadInt32(&factoryCalls))

	// Release with 150ms grace period
	gracePeriod := 150 * time.Millisecond
	pool.release(key, gracePeriod, nil)
	assert.True(t, pool.hasLingerTimer(key))

	// Sleep 30ms (well within grace period), then re-acquire
	time.Sleep(30 * time.Millisecond)
	c2, _, err := pool.acquire(key, nil, factory)
	require.NoError(t, err)

	// Timer should be cancelled, factory NOT called again, same client returned
	assert.Same(t, c1, c2)
	assert.Equal(t, int32(1), atomic.LoadInt32(&factoryCalls))
	assert.Equal(t, 1, pool.getRefCount(key))
	assert.False(t, pool.hasLingerTimer(key))

	// Sleep longer than original grace period; client must still be alive!
	time.Sleep(gracePeriod + 50*time.Millisecond)
	assert.NoError(t, c1.Ping(context.Background()).Err())
	assert.Equal(t, 1, pool.len())

	// Finally release with 0 grace period to clean up
	pool.release(key, 0, nil)
	assert.Equal(t, 0, pool.len())
}

func TestClientPool_DistinctKeys(t *testing.T) {
	mr, err := miniredis.Run()
	require.NoError(t, err)
	defer mr.Close()

	pool := newRedisClientPool()
	defer pool.reset()

	factory := func() (redis.UniversalClient, *redislock.Client, error) {
		c := redis.NewClient(&redis.Options{Addr: mr.Addr()})
		return c, redislock.New(c), nil
	}

	key1 := poolIdentity{ClientType: "simple", Addrs: "test-distinct-key1"}
	key2 := poolIdentity{ClientType: "simple", Addrs: "test-distinct-key2"}

	c1, _, err := pool.acquire(key1, nil, factory)
	require.NoError(t, err)
	c2, _, err := pool.acquire(key2, nil, factory)
	require.NoError(t, err)

	assert.NotSame(t, c1, c2)
	assert.Equal(t, 2, pool.len())

	pool.release(key1, 0, nil)
	assert.Equal(t, 1, pool.len())
	assert.NoError(t, c2.Ping(context.Background()).Err())

	pool.release(key2, 0, nil)
	assert.Equal(t, 0, pool.len())
}

func TestRedisStorage_LifecycleIntegration_ReloadSharing(t *testing.T) {
	mr, err := miniredis.Run()
	require.NoError(t, err)
	defer mr.Close()
	defer defaultPool.reset()

	ctx := context.Background()
	logger, _ := zap.NewProduction()

	// Provision old config (rs1)
	rs1 := New()
	rs1.logger = logger.Sugar()
	rs1.Address = []string{mr.Addr()}
	rs1.DB = DBIndex("0")
	rs1.KeyPrefix = "caddy"
	err = rs1.finalizeConfiguration(ctx)
	require.NoError(t, err)

	// Store data via rs1
	err = rs1.Store(ctx, "cert/example.com", []byte("certificate-bytes"))
	require.NoError(t, err)

	// Provision new config (rs2) simulating a Caddy reload with same Redis config
	rs2 := New()
	rs2.logger = logger.Sugar()
	rs2.Address = []string{mr.Addr()}
	rs2.DB = DBIndex("0")
	rs2.KeyPrefix = "caddy"
	err = rs2.finalizeConfiguration(ctx)
	require.NoError(t, err)

	// Both instances must share the underlying Redis client
	assert.Same(t, rs1.client, rs2.client)
	assert.Same(t, rs1.GetClient(), rs2.GetClient())

	// Old configuration is cleaned up
	err = rs1.Cleanup()
	require.NoError(t, err)

	// rs2 should continue working without interruption
	val, err := rs2.Load(ctx, "cert/example.com")
	require.NoError(t, err)
	assert.Equal(t, []byte("certificate-bytes"), val)

	// Cleanup rs2
	err = rs2.Cleanup()
	require.NoError(t, err)
}

func TestRedisStorage_DoubleCleanup(t *testing.T) {
	mr, err := miniredis.Run()
	require.NoError(t, err)
	defer mr.Close()
	defer defaultPool.reset()

	ctx := context.Background()

	// Provision rs1
	rs1 := New()
	rs1.Address = []string{mr.Addr()}
	rs1.DB = DBIndex("0")
	err = rs1.finalizeConfiguration(ctx)
	require.NoError(t, err)

	// Provision rs2 (shares pooled client, refCount=2)
	rs2 := New()
	rs2.Address = []string{mr.Addr()}
	rs2.DB = DBIndex("0")
	err = rs2.finalizeConfiguration(ctx)
	require.NoError(t, err)

	// Verify both share the client and refCount is 2
	assert.Same(t, rs1.client, rs2.client)
	assert.Equal(t, 2, defaultPool.getRefCount(rs1.poolKeyVal))

	// Call Cleanup() twice on rs1
	err = rs1.Cleanup()
	require.NoError(t, err)
	err = rs1.Cleanup()
	require.NoError(t, err)

	// refCount must be 1, NOT 0 (second Cleanup is idempotent)
	assert.Equal(t, 1, defaultPool.getRefCount(rs2.poolKeyVal))

	// rs2 must still be functional
	assert.NoError(t, rs2.client.Ping(ctx).Err())

	err = rs2.Store(ctx, "k", []byte("v"))
	assert.NoError(t, err)

	_ = rs2.Cleanup()
}

func TestRedisStorage_DelayedShutdown_BackgroundOperation(t *testing.T) {
	mr, err := miniredis.Run()
	require.NoError(t, err)
	defer mr.Close()
	defer defaultPool.reset()

	ctx := context.Background()
	logger, _ := zap.NewProduction()

	rs := New()
	rs.logger = logger.Sugar()
	rs.Address = []string{mr.Addr()}
	rs.DB = DBIndex("0")
	rs.GracePeriod = "100ms"
	err = rs.finalizeConfiguration(ctx)
	require.NoError(t, err)

	err = rs.Store(ctx, "background-key", []byte("background-value"))
	require.NoError(t, err)

	clientRef := rs.client

	// Trigger Cleanup (initiating delayed shutdown)
	err = rs.Cleanup()
	require.NoError(t, err)

	// During the grace period, background routine can still load data
	val, err := rs.Load(ctx, "background-key")
	require.NoError(t, err)
	assert.Equal(t, []byte("background-value"), val)

	// Wait for grace period to expire
	time.Sleep(160 * time.Millisecond)

	// After grace period, client should be closed
	err = clientRef.Ping(ctx).Err()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "client is closed")
}

func TestRedisStorage_UnlockAfterCleanup(t *testing.T) {
	mr, err := miniredis.Run()
	require.NoError(t, err)
	defer mr.Close()
	defer defaultPool.reset()

	ctx := context.Background()
	logger, _ := zap.NewProduction()

	rs := New()
	rs.logger = logger.Sugar()
	rs.Address = []string{mr.Addr()}
	rs.DB = DBIndex("0")
	rs.GracePeriod = "200ms"
	err = rs.finalizeConfiguration(ctx)
	require.NoError(t, err)

	lockKey := "issue_cert_example.com"
	err = rs.Lock(ctx, lockKey)
	require.NoError(t, err)

	// Emulate Caddy process exit sequence:
	// 1. Module Cleanup() is called
	err = rs.Cleanup()
	require.NoError(t, err)

	// 2. certmagic.CleanUpOwnLocks calls Unlock() on the storage
	err = rs.Unlock(ctx, lockKey)
	require.NoError(t, err)

	// Verify the lock was actually released and can be acquired again
	err = rs.Lock(ctx, lockKey)
	require.NoError(t, err)
	_ = rs.Unlock(ctx, lockKey)
}

func TestRedisStorage_GracePeriodConfiguration(t *testing.T) {
	mr, err := miniredis.Run()
	require.NoError(t, err)
	defer mr.Close()
	defer defaultPool.reset()

	t.Run("default grace_period is 30s", func(t *testing.T) {
		rs := New()
		rs.Address = []string{mr.Addr()}
		err := rs.finalizeConfiguration(context.Background())
		require.NoError(t, err)
		assert.Equal(t, 30*time.Second, rs.gracePeriodDuration)
		_ = rs.Cleanup()
	})

	t.Run("custom duration string parsed", func(t *testing.T) {
		rs := New()
		rs.Address = []string{mr.Addr()}
		rs.GracePeriod = "15s"
		err := rs.finalizeConfiguration(context.Background())
		require.NoError(t, err)
		assert.Equal(t, 15*time.Second, rs.gracePeriodDuration)
		_ = rs.Cleanup()
	})

	t.Run("custom integer seconds parsed", func(t *testing.T) {
		rs := New()
		rs.Address = []string{mr.Addr()}
		rs.GracePeriod = "10"
		err := rs.finalizeConfiguration(context.Background())
		require.NoError(t, err)
		assert.Equal(t, 10*time.Second, rs.gracePeriodDuration)
		_ = rs.Cleanup()
	})

	t.Run("zero grace_period parsed", func(t *testing.T) {
		rs := New()
		rs.Address = []string{mr.Addr()}
		rs.GracePeriod = "0s"
		err := rs.finalizeConfiguration(context.Background())
		require.NoError(t, err)
		assert.Equal(t, time.Duration(0), rs.gracePeriodDuration)
		_ = rs.Cleanup()
	})

	t.Run("invalid grace_period rejected", func(t *testing.T) {
		rs := New()
		rs.Address = []string{mr.Addr()}
		rs.GracePeriod = "invalid-duration"
		err := rs.finalizeConfiguration(context.Background())
		require.Error(t, err)
		assert.Contains(t, err.Error(), "invalid grace_period value")
	})

	t.Run("caddyfile unmarshals grace_period", func(t *testing.T) {
		d := caddyfile.NewTestDispenser(`
			redis {
				address 127.0.0.1:6379
				grace_period 45s
			}
		`)
		rs := New()
		err := rs.UnmarshalCaddyfile(d)
		require.NoError(t, err)
		assert.Equal(t, "45s", rs.GracePeriod)
	})
}

// Test helpers for inspecting and cleaning up redisClientPool during tests.

func (p *redisClientPool) getRefCount(key poolIdentity) int {
	p.mu.Lock()
	defer p.mu.Unlock()
	if entry, exists := p.entries[key]; exists {
		return entry.refCount
	}
	return 0
}

func (p *redisClientPool) hasLingerTimer(key poolIdentity) bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	if entry, exists := p.entries[key]; exists {
		return entry.lingerTimer != nil
	}
	return false
}

func (p *redisClientPool) len() int {
	p.mu.Lock()
	defer p.mu.Unlock()
	return len(p.entries)
}

func (p *redisClientPool) reset() {
	p.mu.Lock()
	defer p.mu.Unlock()
	for k, entry := range p.entries {
		if entry.lingerTimer != nil {
			entry.lingerTimer.Stop()
		}
		_ = entry.client.Close()
		delete(p.entries, k)
	}
}
