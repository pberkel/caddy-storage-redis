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
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/bsm/redislock"
	"github.com/redis/go-redis/v9"
	"go.uber.org/zap"
)

const (
	defaultClientShutdownGracePeriodStr = "10s"
	defaultClientShutdownGracePeriod    = 10 * time.Second
)

type pooledClientEntry struct {
	client          redis.UniversalClient
	locker          *redislock.Client
	refCount        int
	lingerTimer     *time.Timer
	closeAfter      time.Time
	timerGeneration uint64
}

// poolIdentity is the connection identity used to key the client pool - two
// RedisStorage configs share a client iff they produce an identical
// poolIdentity. Deliberately narrower than the full RedisStorage struct:
// fields like KeyPrefix/Compression/EncryptionKey don't affect the
// underlying go-redis client, so configs differing only in those should
// still share a connection.
type poolIdentity struct {
	ClientType         string
	Addrs              string // pre-sorted, comma-joined - see poolKey()
	DB                 DBIndex
	Timeout            string
	Username           string
	Password           string
	SentinelPassword   string
	MasterName         string
	TlsEnabled         bool
	TlsInsecure        bool
	TlsServerCertsPEM  string
	TlsServerCertsPath string
	TlsTrustPEM        string
	RouteByLatency     bool
	RouteRandomly      bool
	// A client that skipped the probe must never be handed to an instance that asked for it.
	SkipConnectionCheck bool
}

func (pi poolIdentity) String() string {
	return fmt.Sprintf("%s|%s|%s", pi.ClientType, pi.Addrs, pi.DB)
}

func (rs *RedisStorage) poolKey() poolIdentity {
	addrs := make([]string, len(rs.Address))
	copy(addrs, rs.Address)
	sort.Strings(addrs)

	return poolIdentity{
		ClientType:          rs.ClientType,
		Addrs:               strings.Join(addrs, ","),
		DB:                  rs.DB,
		Timeout:             rs.Timeout,
		Username:            rs.Username,
		Password:            rs.Password,
		SentinelPassword:    rs.SentinelPassword,
		MasterName:          rs.MasterName,
		TlsEnabled:          rs.TlsEnabled,
		TlsInsecure:         rs.TlsInsecure,
		TlsServerCertsPEM:   rs.TlsServerCertsPEM,
		TlsServerCertsPath:  rs.TlsServerCertsPath,
		RouteByLatency:      rs.RouteByLatency,
		RouteRandomly:       rs.RouteRandomly,
		SkipConnectionCheck: rs.SkipConnectionCheck,
	}
}

type redisClientPool struct {
	mu      sync.Mutex
	entries map[poolIdentity]*pooledClientEntry
}

var defaultPool = newRedisClientPool()

func newRedisClientPool() *redisClientPool {
	return &redisClientPool{
		entries: make(map[poolIdentity]*pooledClientEntry),
	}
}

func (p *redisClientPool) acquire(
	key poolIdentity,
	logger *zap.SugaredLogger,
	factory func() (redis.UniversalClient, *redislock.Client, error),
) (redis.UniversalClient, *redislock.Client, error) {
	if logger == nil {
		logger = zap.NewNop().Sugar()
	}

	p.mu.Lock()
	defer p.mu.Unlock()

	if entry, exists := p.entries[key]; exists {
		entry.timerGeneration++ // Invalidate callbacks that have already started.
		if entry.lingerTimer != nil {
			if entry.lingerTimer.Stop() {
				logger.Debugf("Cancelled delayed shutdown for pooled Redis client (%s)", key)
			}
			entry.lingerTimer = nil
		}
		entry.refCount++
		logger.Debugf("Reused pooled Redis client (%s), refCount: %d", key, entry.refCount)
		return entry.client, entry.locker, nil
	}

	client, locker, err := factory()
	if err != nil {
		return nil, nil, err
	}

	p.entries[key] = &pooledClientEntry{
		client:   client,
		locker:   locker,
		refCount: 1,
	}
	logger.Debugf("Created new pooled Redis client (%s), refCount: 1", key)
	return client, locker, nil
}

func (p *redisClientPool) retain(key poolIdentity) bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	entry, exists := p.entries[key]
	if !exists || entry.refCount == 0 {
		return false
	}
	entry.refCount++
	return true
}

func (p *redisClientPool) release(
	key poolIdentity,
	clientShutdownGracePeriod time.Duration,
	logger *zap.SugaredLogger,
) error {
	if logger == nil {
		logger = zap.NewNop().Sugar()
	}

	p.mu.Lock()
	entry, exists := p.entries[key]
	if !exists {
		p.mu.Unlock()
		return nil
	}

	// Preserve each released owner's deadline, even while other owners remain.
	deadline := time.Now().Add(max(clientShutdownGracePeriod, 0))
	if deadline.After(entry.closeAfter) {
		entry.closeAfter = deadline
	}
	entry.refCount--
	logger.Debugf("Released pooled Redis client (%s), refCount: %d", key, entry.refCount)

	if entry.refCount > 0 {
		p.mu.Unlock()
		return nil
	}

	entry.timerGeneration++
	generation := entry.timerGeneration
	if entry.lingerTimer != nil {
		entry.lingerTimer.Stop()
		entry.lingerTimer = nil
	}
	delay := time.Until(entry.closeAfter)
	if delay <= 0 {
		delete(p.entries, key)
		p.mu.Unlock()
		err := entry.client.Close()
		if err != nil {
			logger.Warnf("Error closing Redis client (%s): %v", key, err)
		}
		logger.Debugf("Closed Redis client immediately (%s)", key)
		return err
	}

	logger.Debugf("Scheduled delayed shutdown for Redis client (%s) in %v", key, delay)

	entry.lingerTimer = time.AfterFunc(delay, func() {
		p.closeIdleClient(key, entry, generation, logger)
	})
	p.mu.Unlock()
	return nil
}

func (p *redisClientPool) closeIdleClient(key poolIdentity, entry *pooledClientEntry, generation uint64, logger *zap.SugaredLogger) {
	p.mu.Lock()
	current, exists := p.entries[key]
	if !exists || current != entry || entry.refCount != 0 || entry.timerGeneration != generation {
		p.mu.Unlock()
		return
	}
	delete(p.entries, key)
	entry.lingerTimer = nil
	p.mu.Unlock()

	if err := entry.client.Close(); err != nil {
		logger.Warnf("Error closing Redis client after grace period (%s): %v", key, err)
	}
	logger.Infof("Closed Redis client after grace period (%s)", key)
}
