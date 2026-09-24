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
	"errors"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRedisStorage_AcquireClientLifetime(t *testing.T) {
	rs, ctx := provisionRedisStorage(t)
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
	rs, ctx := provisionRedisStorage(t)
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
	old, ctx := provisionRedisStorage(t)
	value, release, err := old.AcquireClient()
	require.NoError(t, err)
	t.Cleanup(func() { assert.NoError(t, release()) })
	client := value.(redis.UniversalClient)

	replacement, _ := provisionRedisStorage(t)
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
	rs := &RedisStorage{client: client, clientLifetime: &clientLifetime{client: client, refs: 1}}
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
		rs := &RedisStorage{client: client, clientLifetime: &clientLifetime{client: client, refs: 1}}
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
