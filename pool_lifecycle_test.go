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
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/json"
	"encoding/pem"
	"math/big"
	"net"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"testing/synctest"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/alicebob/miniredis/v2/server"
	"github.com/bsm/redislock"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

func denyRedisPing(mr *miniredis.Miniredis) {
	mr.Server().SetPreHook(func(p *server.Peer, cmd string, args ...string) bool {
		if strings.EqualFold(cmd, "ping") {
			p.WriteError("NOPERM PING denied")
			return true
		}
		return false
	})
}

func TestRedisStorage_FailedPingClosesClient(t *testing.T) {
	mr := miniredis.RunT(t)
	denyRedisPing(mr)
	for i := 0; i < 3; i++ {
		rs := New()
		rs.Address = []string{mr.Addr()}
		require.ErrorContains(t, rs.finalizeConfiguration(context.Background()), "NOPERM")
		require.NoError(t, rs.Cleanup())
		require.Eventually(t, func() bool { return mr.CurrentConnectionCount() == 0 }, time.Second, time.Millisecond)
	}
}

func makeRedisTLSCertificate(t *testing.T, serial int64) (tls.Certificate, []byte) {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	require.NoError(t, err)
	template := &x509.Certificate{
		SerialNumber: big.NewInt(serial),
		Subject:      pkix.Name{CommonName: "Redis test server"},
		NotBefore:    time.Now().Add(-time.Hour), NotAfter: time.Now().Add(time.Hour),
		IPAddresses: []net.IP{net.ParseIP("127.0.0.1")},
		KeyUsage:    x509.KeyUsageDigitalSignature | x509.KeyUsageCertSign,
		ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		IsCA:        true, BasicConstraintsValid: true,
	}
	der, err := x509.CreateCertificate(rand.Reader, template, template, &key.PublicKey, key)
	require.NoError(t, err)
	return tls.Certificate{Certificate: [][]byte{der}, PrivateKey: key}, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})
}

func TestRedisStorage_TLSFileReload(t *testing.T) {
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
		return rs
	}
	a := newStorage()
	require.NoError(t, a.finalizeConfiguration(context.Background()))
	unchanged := newStorage()
	require.NoError(t, unchanged.finalizeConfiguration(context.Background()))
	require.Same(t, a.client, unchanged.client)

	activeCert.Store(&certB)
	require.NoError(t, os.WriteFile(certPath, pemB, 0600))
	b := newStorage()
	require.NoError(t, b.finalizeConfiguration(context.Background()))
	require.NotSame(t, a.client, b.client)
	require.NoError(t, b.client.Ping(context.Background()).Err())
	require.False(t, a.client.(*redis.Client).Options().TLSConfig.RootCAs.Equal(b.client.(*redis.Client).Options().TLSConfig.RootCAs))

	require.NoError(t, os.WriteFile(certPath, []byte("invalid PEM"), 0600))
	require.ErrorContains(t, newStorage().finalizeConfiguration(context.Background()), "Failed to load PEM server certs")
	require.NoError(t, os.Remove(certPath))
	require.ErrorContains(t, newStorage().finalizeConfiguration(context.Background()), "Failed to load PEM server certs from file")
	require.NoError(t, a.Cleanup())
	require.NoError(t, unchanged.Cleanup())
	require.NoError(t, b.Cleanup())
	require.Equal(t, 0, defaultPool.getRefCount(a.poolKeyVal))
	require.Equal(t, 0, defaultPool.getRefCount(b.poolKeyVal))
}

func TestRedisStorage_CredentialPoolIsolation(t *testing.T) {
	mr := miniredis.RunT(t)
	mr.RequireUserAuth("alice|extra", "pw")
	mr.RequireUserAuth("alice", "extra|pw")
	defer defaultPool.reset()
	a, b := New(), New()
	a.Address, b.Address = []string{mr.Addr()}, []string{mr.Addr()}
	a.Username, a.Password = "alice|extra", "pw"
	b.Username, b.Password = "alice", "extra|pw"
	require.NoError(t, a.finalizeConfiguration(context.Background()))
	require.NoError(t, b.finalizeConfiguration(context.Background()))
	require.NotSame(t, a.client, b.client)
	require.Equal(t, "alice|extra", a.client.(*redis.Client).Options().Username)
	require.Equal(t, "alice", b.client.(*redis.Client).Options().Username)
}

func TestRedisStorage_ConcurrentCleanup(t *testing.T) {
	mr := miniredis.RunT(t)
	defer defaultPool.reset()
	a, b := New(), New()
	a.Address, b.Address = []string{mr.Addr()}, []string{mr.Addr()}
	require.NoError(t, a.finalizeConfiguration(context.Background()))
	require.NoError(t, b.finalizeConfiguration(context.Background()))
	var wg sync.WaitGroup
	for i := 0; i < 32; i++ {
		wg.Add(1)
		go func() { defer wg.Done(); _ = a.Cleanup() }()
	}
	wg.Wait()
	require.Equal(t, 1, defaultPool.getRefCount(b.poolKeyVal))
	require.NoError(t, b.client.Ping(context.Background()).Err())
	require.NoError(t, a.client.Ping(context.Background()).Err())
}

func TestRedisStorage_ClientShutdownGracePeriodJSON(t *testing.T) {
	rs := New()
	require.NoError(t, json.Unmarshal([]byte(`{"client_shutdown_grace_period":"45s"}`), rs))
	require.Equal(t, "45s", rs.ClientShutdownGracePeriod)
	encoded, err := json.Marshal(rs)
	require.NoError(t, err)
	require.Contains(t, string(encoded), `"client_shutdown_grace_period":"45s"`)
	require.NotContains(t, string(encoded), `"grace_period"`)
}

type poolTestClient struct {
	redis.UniversalClient
	closes atomic.Int32
}

func (c *poolTestClient) Close() error { c.closes.Add(1); return nil }

func TestClientPool_OwnerShutdownDeadlines(t *testing.T) {
	for _, tc := range []struct {
		name                               string
		first, second, interval, remaining time.Duration
	}{
		{"long then zero", 30 * time.Second, 0, 5 * time.Second, 25 * time.Second},
		{"zero then long", 0, 30 * time.Second, 5 * time.Second, 30 * time.Second},
		{"elapsed deadline", 5 * time.Second, 0, 10 * time.Second, 0},
		{"both zero", 0, 0, 0, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				pool := newRedisClientPool()
				client := &poolTestClient{}
				key := poolIdentity{ClientType: "simple"}
				factory := func() (redis.UniversalClient, *redislock.Client, error) { return client, nil, nil }
				_, _, err := pool.acquire(key, nil, factory)
				require.NoError(t, err)
				_, _, err = pool.acquire(key, nil, factory)
				require.NoError(t, err)
				pool.release(key, tc.first, nil)
				time.Sleep(tc.interval)
				pool.release(key, tc.second, nil)
				if tc.remaining > 0 {
					time.Sleep(tc.remaining - time.Nanosecond)
					synctest.Wait()
					require.Equal(t, int32(0), client.closes.Load())
					time.Sleep(time.Nanosecond)
				}
				synctest.Wait()
				require.Equal(t, int32(1), client.closes.Load())
				require.Equal(t, 0, pool.len())
			})
		})
	}
}

func TestClientPool_StaleShutdownCallback(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		pool := newRedisClientPool()
		client := &poolTestClient{}
		key := poolIdentity{ClientType: "simple"}
		factory := func() (redis.UniversalClient, *redislock.Client, error) { return client, nil, nil }
		_, _, err := pool.acquire(key, nil, factory)
		require.NoError(t, err)
		pool.release(key, 30*time.Second, nil)
		pool.mu.Lock()
		entry := pool.entries[key]
		oldGeneration := entry.timerGeneration
		entry.lingerTimer.Stop()
		pool.mu.Unlock()

		// Hold the old callback until a later release has installed a new timer.
		resume := make(chan struct{})
		go func() {
			<-resume
			pool.closeIdleClient(key, entry, oldGeneration, zap.NewNop().Sugar())
		}()
		time.Sleep(30 * time.Second)
		reused, _, err := pool.acquire(key, nil, factory)
		require.NoError(t, err)
		require.Same(t, client, reused)
		pool.release(key, 30*time.Second, nil)
		close(resume)
		synctest.Wait()
		require.Equal(t, int32(0), client.closes.Load())
		time.Sleep(30*time.Second - time.Nanosecond)
		synctest.Wait()
		require.Equal(t, int32(0), client.closes.Load())
		time.Sleep(time.Nanosecond)
		synctest.Wait()
		require.Equal(t, int32(1), client.closes.Load())
	})
}

func TestClientPool_ReacquirePreservesShutdownDeadline(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		pool := newRedisClientPool()
		client := &poolTestClient{}
		key := poolIdentity{ClientType: "simple"}
		factory := func() (redis.UniversalClient, *redislock.Client, error) { return client, nil, nil }
		_, _, err := pool.acquire(key, nil, factory)
		require.NoError(t, err)
		pool.release(key, 30*time.Second, nil)
		time.Sleep(5 * time.Second)
		_, _, err = pool.acquire(key, nil, factory)
		require.NoError(t, err)
		pool.release(key, 0, nil)
		time.Sleep(25*time.Second - time.Nanosecond)
		synctest.Wait()
		require.Equal(t, int32(0), client.closes.Load())
		time.Sleep(time.Nanosecond)
		synctest.Wait()
		require.Equal(t, int32(1), client.closes.Load())
	})
}
