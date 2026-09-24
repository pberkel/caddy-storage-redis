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

	"github.com/redis/go-redis/v9"
)

// ErrClientUnavailable indicates that storage is unprovisioned or has been cleaned up.
var ErrClientUnavailable = errors.New("Redis storage client is not available")

// AcquireClient retains the client until the returned release function is called.
// Call it after provisioning and before Cleanup. The client must not be closed directly.
// AcquireClient, Cleanup, and release may run concurrently; release is idempotent.
// Like GetClient, the returned client is usually cast to redis.UniversalClient.
func (rs *RedisStorage) AcquireClient() (client any, release func() error, err error) {
	lifetime := rs.clientLifetime
	if lifetime == nil {
		return nil, nil, ErrClientUnavailable
	}

	lifetime.mu.Lock()
	defer lifetime.mu.Unlock()
	if lifetime.cleaned {
		return nil, nil, ErrClientUnavailable
	}
	lifetime.refs++

	return lifetime.client, sync.OnceValue(lifetime.release), nil
}

// Kept behind a pointer because RedisStorage has value receivers.
type clientLifetime struct {
	mu          sync.Mutex
	client      redis.UniversalClient
	refs        int
	cleaned     bool
	cleanupOnce sync.Once
	cleanupErr  error
}

func (l *clientLifetime) cleanup() error {
	l.cleanupOnce.Do(func() {
		l.mu.Lock()
		l.cleaned = true
		l.mu.Unlock()
		l.cleanupErr = l.release()
	})
	return l.cleanupErr
}

func (l *clientLifetime) release() error {
	l.mu.Lock()
	l.refs--
	last := l.refs == 0
	l.mu.Unlock()
	if last {
		return l.client.Close()
	}
	return nil
}
