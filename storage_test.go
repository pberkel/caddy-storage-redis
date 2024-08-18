package storageredis

import (
	"context"
	"encoding/json"
	"errors"
	"io/fs"
	"strconv"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"go.uber.org/zap"
)

const (
	TestDB            = 9
	TestKeyPrefix     = "redistlstest"
	TestEncryptionKey = "1aedfs5kcM8lOZO3BDDMuwC23croDwRr"
	TestCompression   = true

	TestKeyCertPath       = "certificates"
	TestKeyAcmePath       = TestKeyCertPath + "/acme-v02.api.letsencrypt.org-directory"
	TestKeyExamplePath    = TestKeyAcmePath + "/example.com"
	TestKeyExampleCrt     = TestKeyExamplePath + "/example.com.crt"
	TestKeyExampleKey     = TestKeyExamplePath + "/example.com.key"
	TestKeyExampleJson    = TestKeyExamplePath + "/example.com.json"
	TestKeyLock           = "locks/issue_cert_example.com"
	TestKeyLockIterations = 250
)

var (
	TestValueCrt  = []byte("TG9yZW0gaXBzdW0gZG9sb3Igc2l0IGFtZXQsIGNvbnNlY3RldHVyIGFkaXBpc2NpbmcgZWxpdCwgc2VkIGRvIGVpdXNtb2QgdGVtcG9yIGluY2lkaWR1bnQgdXQgbGFib3JlIGV0IGRvbG9yZSBtYWduYSBhbGlxdWEu")
	TestValueKey  = []byte("RWdlc3RhcyBlZ2VzdGFzIGZyaW5naWxsYSBwaGFzZWxsdXMgZmF1Y2lidXMgc2NlbGVyaXNxdWUgZWxlaWZlbmQgZG9uZWMgcHJldGl1bSB2dWxwdXRhdGUuIFRpbmNpZHVudCBvcm5hcmUgbWFzc2EgZWdldC4=")
	TestValueJson = []byte("U2FnaXR0aXMgYWxpcXVhbSBtYWxlc3VhZGEgYmliZW5kdW0gYXJjdSB2aXRhZSBlbGVtZW50dW0uIEludGVnZXIgbWFsZXN1YWRhIG51bmMgdmVsIHJpc3VzIGNvbW1vZG8gdml2ZXJyYSBtYWVjZW5hcy4=")
)

// Emulate the Provision() Caddy function
func provisionRedisStorage(t *testing.T) (*RedisStorage, context.Context) {

	ctx := context.Background()
	rs := New()

	logger, _ := zap.NewProduction()
	rs.logger = logger.Sugar()

	rs.DB = TestDB
	rs.KeyPrefix = TestKeyPrefix
	rs.EncryptionKey = TestEncryptionKey
	rs.Compression = TestCompression

	err := rs.finalizeConfiguration(ctx)
	assert.NoError(t, err)

	// Skip test if unable to connect to Redis server
	if err != nil {
		t.Skip()
		return nil, nil
	}

	// Flush the current Redis database
	err = rs.Client.FlushDB(ctx).Err()
	assert.NoError(t, err)

	return rs, ctx
}

func TestRedisStorage_Store(t *testing.T) {

	rs, ctx := provisionRedisStorage(t)

	err := rs.Store(ctx, TestKeyExampleCrt, TestValueCrt)
	assert.NoError(t, err)
}

func TestRedisStorage_Exists(t *testing.T) {

	rs, ctx := provisionRedisStorage(t)

	exists := rs.Exists(ctx, TestKeyExampleCrt)
	assert.False(t, exists)

	err := rs.Store(ctx, TestKeyExampleCrt, TestValueCrt)
	assert.NoError(t, err)

	exists = rs.Exists(ctx, TestKeyExampleCrt)
	assert.True(t, exists)
}

func TestRedisStorage_Load(t *testing.T) {

	rs, ctx := provisionRedisStorage(t)

	err := rs.Store(ctx, TestKeyExampleCrt, TestValueCrt)
	assert.NoError(t, err)

	loadedValue, err := rs.Load(ctx, TestKeyExampleCrt)
	assert.NoError(t, err)

	assert.Equal(t, TestValueCrt, loadedValue)
}

func TestRedisStorage_Delete(t *testing.T) {

	rs, ctx := provisionRedisStorage(t)

	err := rs.Store(ctx, TestKeyExampleCrt, TestValueCrt)
	assert.NoError(t, err)

	err = rs.Delete(ctx, TestKeyExampleCrt)
	assert.NoError(t, err)

	exists := rs.Exists(ctx, TestKeyExampleCrt)
	assert.False(t, exists)

	loadedValue, err := rs.Load(ctx, TestKeyExampleCrt)
	assert.Nil(t, loadedValue)

	notExist := errors.Is(err, fs.ErrNotExist)
	assert.True(t, notExist)
}

func TestRedisStorage_Stat(t *testing.T) {

	rs, ctx := provisionRedisStorage(t)
	size := int64(len(TestValueCrt))

	startTime := time.Now()
	err := rs.Store(ctx, TestKeyExampleCrt, TestValueCrt)
	endTime := time.Now()
	assert.NoError(t, err)

	stat, err := rs.Stat(ctx, TestKeyExampleCrt)
	assert.NoError(t, err)

	assert.Equal(t, TestKeyExampleCrt, stat.Key)
	assert.WithinRange(t, stat.Modified, startTime, endTime)
	assert.Equal(t, size, stat.Size)
}

func TestRedisStorage_List(t *testing.T) {

	rs, ctx := provisionRedisStorage(t)

	// Store two key values
	err := rs.Store(ctx, TestKeyExampleCrt, TestValueCrt)
	assert.NoError(t, err)

	err = rs.Store(ctx, TestKeyExampleKey, TestValueKey)
	assert.NoError(t, err)

	// List recursively from root
	keys, err := rs.List(ctx, "", true)
	assert.NoError(t, err)
	assert.Len(t, keys, 2)
	assert.Contains(t, keys, TestKeyExampleCrt)
	assert.Contains(t, keys, TestKeyExampleKey)
	assert.NotContains(t, keys, TestKeyExampleJson)

	// List recursively from first directory
	keys, err = rs.List(ctx, TestKeyCertPath, true)
	assert.NoError(t, err)
	assert.Len(t, keys, 2)
	assert.Contains(t, keys, TestKeyExampleCrt)
	assert.Contains(t, keys, TestKeyExampleKey)
	assert.NotContains(t, keys, TestKeyExampleJson)

	// Store third key value
	err = rs.Store(ctx, TestKeyExampleJson, TestValueJson)
	assert.NoError(t, err)

	// List recursively from root
	keys, err = rs.List(ctx, "", true)
	assert.NoError(t, err)
	assert.Len(t, keys, 3)
	assert.Contains(t, keys, TestKeyExampleCrt)
	assert.Contains(t, keys, TestKeyExampleKey)
	assert.Contains(t, keys, TestKeyExampleJson)

	// List recursively from first directory
	keys, err = rs.List(ctx, TestKeyCertPath, true)
	assert.NoError(t, err)
	assert.Len(t, keys, 3)
	assert.Contains(t, keys, TestKeyExampleCrt)
	assert.Contains(t, keys, TestKeyExampleKey)
	assert.Contains(t, keys, TestKeyExampleJson)

	// Delete one key value
	err = rs.Delete(ctx, TestKeyExampleCrt)
	assert.NoError(t, err)

	// List recursively from root
	keys, err = rs.List(ctx, "", true)
	assert.NoError(t, err)
	assert.Len(t, keys, 2)
	assert.NotContains(t, keys, TestKeyExampleCrt)
	assert.Contains(t, keys, TestKeyExampleKey)
	assert.Contains(t, keys, TestKeyExampleJson)

	keys, err = rs.List(ctx, TestKeyCertPath, true)
	assert.NoError(t, err)
	assert.Len(t, keys, 2)
	assert.NotContains(t, keys, TestKeyExampleCrt)
	assert.Contains(t, keys, TestKeyExampleKey)
	assert.Contains(t, keys, TestKeyExampleJson)

	// Delete remaining two key values
	err = rs.Delete(ctx, TestKeyExampleKey)
	assert.NoError(t, err)

	err = rs.Delete(ctx, TestKeyExampleJson)
	assert.NoError(t, err)

	// List recursively from root
	keys, err = rs.List(ctx, "", true)
	assert.NoError(t, err)
	assert.Empty(t, keys)

	keys, err = rs.List(ctx, TestKeyCertPath, true)
	assert.NoError(t, err)
	assert.Empty(t, keys)
}

func TestRedisStorage_ListNonRecursive(t *testing.T) {

	rs, ctx := provisionRedisStorage(t)

	// Store three key values
	err := rs.Store(ctx, TestKeyExampleCrt, TestValueCrt)
	assert.NoError(t, err)

	err = rs.Store(ctx, TestKeyExampleKey, TestValueKey)
	assert.NoError(t, err)

	err = rs.Store(ctx, TestKeyExampleJson, TestValueJson)
	assert.NoError(t, err)

	// List non-recursively from root
	keys, err := rs.List(ctx, "", false)
	assert.NoError(t, err)
	assert.Len(t, keys, 1)
	assert.Contains(t, keys, TestKeyCertPath)

	// List non-recursively from first level
	keys, err = rs.List(ctx, TestKeyCertPath, false)
	assert.NoError(t, err)
	assert.Len(t, keys, 1)
	assert.Contains(t, keys, TestKeyAcmePath)

	// List non-recursively from second level
	keys, err = rs.List(ctx, TestKeyAcmePath, false)
	assert.NoError(t, err)
	assert.Len(t, keys, 1)
	assert.Contains(t, keys, TestKeyExamplePath)

	// List non-recursively from third level
	keys, err = rs.List(ctx, TestKeyExamplePath, false)
	assert.NoError(t, err)
	assert.Len(t, keys, 3)
	assert.Contains(t, keys, TestKeyExampleCrt)
	assert.Contains(t, keys, TestKeyExampleKey)
	assert.Contains(t, keys, TestKeyExampleJson)
}

func TestRedisStorage_LockUnlock(t *testing.T) {

	rs, ctx := provisionRedisStorage(t)

	err := rs.Lock(ctx, TestKeyLock)
	assert.NoError(t, err)

	err = rs.Unlock(ctx, TestKeyLock)
	assert.NoError(t, err)
}

func TestRedisStorage_MultipleLocks(t *testing.T) {

	var wg sync.WaitGroup
	var rsArray = make([]*RedisStorage, TestKeyLockIterations)

	for i := 0; i < len(rsArray); i++ {
		rsArray[i], _ = provisionRedisStorage(t)
		wg.Add(1)
	}

	for i := 0; i < len(rsArray); i++ {
		suffix := strconv.Itoa(i / 10)
		go lockAndUnlock(t, &wg, rsArray[i], TestKeyLock+"-"+suffix)
	}

	wg.Wait()
}

func lockAndUnlock(t *testing.T, wg *sync.WaitGroup, rs *RedisStorage, key string) {

	defer wg.Done()

	err := rs.Lock(context.Background(), key)
	assert.NoError(t, err)

	err = rs.Unlock(context.Background(), key)
	assert.NoError(t, err)
}

func TestRedisStorage_String(t *testing.T) {

	rs := New()
	redacted := `REDACTED`

	t.Run("Validate password", func(t *testing.T) {
		t.Run("is redacted when set", func(t *testing.T) {
			testrs := New()
			password := "iAmASuperSecurePassword"
			rs.Password = password
			err := json.Unmarshal([]byte(rs.String()), &testrs)
			assert.NoError(t, err)
			assert.Equal(t, redacted, testrs.Password)
			assert.Equal(t, password, rs.Password)
		})
		rs.Password = ""
		t.Run("is empty if not set", func(t *testing.T) {
			err := json.Unmarshal([]byte(rs.String()), &rs)
			assert.NoError(t, err)
			assert.Empty(t, rs.Password)
		})
	})
	t.Run("Validate AES key", func(t *testing.T) {
		t.Run("is redacted when set", func(t *testing.T) {
			testrs := New()
			aeskey := "abcdefghijklmnopqrstuvwxyz123456"
			rs.EncryptionKey = aeskey
			err := json.Unmarshal([]byte(rs.String()), &testrs)
			assert.NoError(t, err)
			assert.Equal(t, redacted, testrs.EncryptionKey)
			assert.Equal(t, aeskey, rs.EncryptionKey)
		})
		rs.EncryptionKey = ""
		t.Run("is empty if not set", func(t *testing.T) {
			err := json.Unmarshal([]byte(rs.String()), &rs)
			assert.NoError(t, err)
			assert.Empty(t, rs.EncryptionKey)
		})
	})
}
