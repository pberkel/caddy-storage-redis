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
)

// ErrClientUnavailable indicates that storage is unprovisioned or has been cleaned up.
var ErrClientUnavailable = errors.New("Redis storage client is not available")

// AcquireClient retains the provisioned client until release is called.
// AcquireClient, Cleanup, and release may run concurrently after provisioning.
// Release is idempotent; callers must not close the client directly.
// Release returns immediate close errors; delayed close errors are logged.
// Like GetClient, the returned value is usually cast to redis.UniversalClient.
func (rs *RedisStorage) AcquireClient() (client any, release func() error, err error) {
	lifetime := rs.clientLifetime
	if lifetime == nil {
		return nil, nil, ErrClientUnavailable
	}

	lifetime.mu.Lock()
	defer lifetime.mu.Unlock()
	pool, key := defaultPool, rs.poolKeyVal
	if lifetime.cleaned || rs.client == nil || !pool.retain(key) {
		return nil, nil, ErrClientUnavailable
	}

	logger := rs.logger
	return rs.client, sync.OnceValue(func() error {
		return pool.release(key, 0, logger)
	}), nil
}

// Shared by value copies of RedisStorage; connection references live in the pool.
type clientLifetime struct {
	mu          sync.Mutex
	cleaned     bool
	cleanupOnce sync.Once
	cleanupErr  error
}
