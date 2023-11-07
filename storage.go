package storageredis

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"path"
	"strings"
	"sync"
	"time"

	"github.com/bsm/redislock"
	"github.com/caddyserver/certmagic"
	"github.com/redis/go-redis/v9"
	"go.uber.org/zap"
)

const (
	// Redis server host
	defaultHost = "127.0.0.1"

	// Redis server port
	defaultPort = "6379"

	// Redis server database
	defaultDb = 0

	// Redis server timeout
	defaultTimeout = 5

	// Prepended to every Redis key
	defaultKeyPrefix = "caddy"

	// Compress values before storing
	defaultCompression = false

	// Connect to Redis via TLS
	defaultTLS = false

	// Do not verify TLS cerficate
	defaultTLSInsecure = true

	// Redis lock time-to-live
	lockTTL = 5 * time.Second

	// Delay between attempts to obtain Lock
	lockPollInterval = 1 * time.Second

	// How frequently the Lock's TTL should be updated
	lockRefreshInterval = 3 * time.Second
)

type RedisStorage struct {
	Address       string `json:"address"`
	Host          string `json:"host"`
	Port          string `json:"port"`
	DB            int    `json:"db"`
	Username      string `json:"username"`
	Password      string `json:"password"`
	Timeout       int    `json:"timeout"`
	KeyPrefix     string `json:"key_prefix"`
	EncryptionKey string `json:"encryption_key"`
	Compression   bool   `json:"compression"`
	TlsEnabled    bool   `json:"tls_enabled"`
	TlsInsecure   bool   `json:"tls_insecure"`

	client *redis.Client
	locker *redislock.Client
	logger *zap.SugaredLogger
	locks  *sync.Map
}

type StorageData struct {
	Value       []byte    `json:"value"`
	Modified    time.Time `json:"modified"`
	Size        int64     `json:"size"`
	Compression int       `json:"compression"`
	Encryption  int       `json:"encryption"`
}

// create a new RedisStorage struct with default values
func NewRedisStorage() *RedisStorage {

	rs := RedisStorage{
		Host:        defaultHost,
		Port:        defaultPort,
		DB:          defaultDb,
		Timeout:     defaultTimeout,
		KeyPrefix:   defaultKeyPrefix,
		Compression: defaultCompression,
		TlsEnabled:  defaultTLS,
		TlsInsecure: defaultTLSInsecure,
	}
	return &rs
}

// Initilalize Redis client and locker
func (rs *RedisStorage) initRedisClient(ctx context.Context) error {

	rs.client = redis.NewClient(&redis.Options{
		Addr:         rs.Address,
		Username:     rs.Username,
		Password:     rs.Password,
		DB:           rs.DB,
		DialTimeout:  time.Duration(rs.Timeout) * time.Second,
		ReadTimeout:  time.Duration(rs.Timeout) * time.Second,
		WriteTimeout: time.Duration(rs.Timeout) * time.Second,
	})

	if rs.TlsEnabled {
		rs.client.Options().TLSConfig = &tls.Config{
			InsecureSkipVerify: rs.TlsInsecure,
		}
	}

	// Test connection to the Redis server
	_, err := rs.client.Ping(ctx).Result()
	if err != nil {
		return err
	}

	rs.locker = redislock.New(rs.client)
	rs.locks = &sync.Map{}
	return nil
}

func (rs RedisStorage) Store(ctx context.Context, key string, value []byte) error {

	var size = len(value)
	var compressionFlag = 0
	var encryptionFlag = 0

	// Compress value if compression enabled
	if rs.Compression {
		compressedValue, err := rs.compress(value)
		if err != nil {
			return fmt.Errorf("Unable to compress value for %s: %v", key, err)
		}
		// Check compression efficiency
		if size > len(compressedValue) {
			value = compressedValue
			compressionFlag = 1
		}
	}

	// Encrypt value if encryption enabled
	if rs.EncryptionKey != "" {
		encryptedValue, err := rs.encrypt(value)
		if err != nil {
			return fmt.Errorf("Unable to encrypt value for %s: %v", key, err)
		}
		value = encryptedValue
		encryptionFlag = 1
	}

	sd := &StorageData{
		Value:       value,
		Modified:    time.Now(),
		Size:        int64(size),
		Compression: compressionFlag,
		Encryption:  encryptionFlag,
	}

	jsonValue, err := json.Marshal(sd)
	if err != nil {
		return fmt.Errorf("Unable to marshal value for %s: %v", key, err)
	}

	var prefixedKey = rs.prefixKey(key)

	// Create directory structure set for current key
	if err := rs.storeDirectoryRecord(ctx, prefixedKey, sd.Modified, false); err != nil {
		return fmt.Errorf("Unable to create directory for key %s: %v", key, err)
	}

	// Store the key value in the Redis database
	if err := rs.client.Set(ctx, prefixedKey, jsonValue, 0).Err(); err != nil {
		return fmt.Errorf("Unable to set value for %s: %v", key, err)
	}

	return nil
}

func (rs RedisStorage) Load(ctx context.Context, key string) ([]byte, error) {

	var sd *StorageData
	var value []byte
	var err error

	sd, err = rs.loadStorageData(ctx, key)
	if err != nil {
		return nil, err
	}
	value = sd.Value

	// Decrypt value if encrypted
	if sd.Encryption > 0 {
		value, err = rs.decrypt(value)
		if err != nil {
			return nil, fmt.Errorf("Unable to decrypt value for %s: %v", key, err)
		}
	}

	// Uncompress value if compressed
	if sd.Compression > 0 {
		value, err = rs.uncompress(value)
		if err != nil {
			return nil, fmt.Errorf("Unable to uncompress value for %s: %v", key, err)
		}
	}

	return value, nil
}

func (rs RedisStorage) Delete(ctx context.Context, key string) error {

	var prefixedKey = rs.prefixKey(key)

	// Remove current key from directory structure
	if err := rs.deleteDirectoryRecord(ctx, prefixedKey, false); err != nil {
		return fmt.Errorf("Unable to delete directory for key %s: %v", key, err)
	}

	if err := rs.client.Del(ctx, prefixedKey).Err(); err != nil {
		return fmt.Errorf("Unable to delete key %s: %v", key, err)
	}

	return nil
}

func (rs RedisStorage) Exists(ctx context.Context, key string) bool {

	// Redis returns a count of the number of keys found
	exists := rs.client.Exists(ctx, rs.prefixKey(key)).Val()

	return exists > 0
}

func (rs RedisStorage) List(ctx context.Context, dir string, recursive bool) ([]string, error) {

	var keyList []string

	// Obtain range of all direct children stored in the Sorted Set
	keys, err := rs.client.ZRange(ctx, rs.prefixKey(dir), 0, -1).Result()
	if err != nil {
		return keyList, fmt.Errorf("Unable to get range of sorted set %s: %v", dir, err)
	}

	// Iterate over each child key
	for _, k := range keys {
		// Directory keys will have a "/" suffix
		trimmedKey := strings.TrimSuffix(k, "/")
		// Reconstruct the full path of child key
		fullPathKey := path.Join(dir, trimmedKey)
		// If current key is a directory
		if recursive && k != trimmedKey {
			// Recursively traverse all child directories
			childKeys, err := rs.List(ctx, fullPathKey, recursive)
			if err != nil {
				return keyList, err
			}
			keyList = append(keyList, childKeys...)
		} else {
			keyList = append(keyList, fullPathKey)
		}
	}

	return keyList, nil
}

func (rs RedisStorage) Stat(ctx context.Context, key string) (certmagic.KeyInfo, error) {

	sd, err := rs.loadStorageData(ctx, key)
	if err != nil {
		return certmagic.KeyInfo{}, err
	}

	return certmagic.KeyInfo{
		Key:        key,
		Modified:   sd.Modified,
		Size:       sd.Size,
		IsTerminal: true,
	}, nil
}

func (rs *RedisStorage) Lock(ctx context.Context, name string) error {

	key := rs.prefixLock(name)

	for {
		// try to obtain lock
		lock, err := rs.locker.Obtain(ctx, key, lockTTL, &redislock.Options{})

		// lock successfully obtained
		if err == nil {
			// store the lock in sync.map, needed for unlocking
			rs.locks.Store(key, lock)
			// keep the lock fresh as long as we hold it
			go func(ctx context.Context, lock *redislock.Lock) {
				for {
					// refresh the Redis lock
					err := lock.Refresh(ctx, lockTTL, nil)
					if err != nil {
						return
					}

					select {
					case <-time.After(lockRefreshInterval):
					case <-ctx.Done():
						return
					}
				}
			}(ctx, lock)

			return nil
		}

		// check for unexpected error
		if err != redislock.ErrNotObtained {
			return fmt.Errorf("Unable to obtain lock for %s: %v", key, err)
		}

		// lock already exists, wait and try again until cancelled
		select {
		case <-time.After(lockPollInterval):
		case <-ctx.Done():
			return ctx.Err()
		}
	}
}

func (rs *RedisStorage) Unlock(ctx context.Context, name string) error {

	key := rs.prefixLock(name)

	// load and delete lock from sync.Map
	if syncMapLock, loaded := rs.locks.LoadAndDelete(key); loaded {

		// type assertion for Redis lock
		if lock, ok := syncMapLock.(*redislock.Lock); ok {

			// release the Redis lock
			if err := lock.Release(ctx); err != nil {
				return fmt.Errorf("Unable to release lock for %s: %v", key, err)
			}
		}
	}

	return nil
}

func (rs *RedisStorage) prefixKey(key string) string {
	return path.Join(rs.KeyPrefix, key)
}

func (rs *RedisStorage) prefixLock(key string) string {
	return rs.prefixKey(path.Join("locks", key))
}

func (rs RedisStorage) loadStorageData(ctx context.Context, key string) (*StorageData, error) {

	data, err := rs.client.Get(ctx, rs.prefixKey(key)).Bytes()
	if data == nil || errors.Is(err, redis.Nil) {
		return nil, fs.ErrNotExist
	} else if err != nil {
		return nil, fmt.Errorf("Unable to get data for %s: %v", key, err)
	}

	sd := &StorageData{}
	if err := json.Unmarshal(data, sd); err != nil {
		return nil, fmt.Errorf("Unable to unmarshal value for %s: %v", key, err)
	}

	return sd, nil
}

func (rs RedisStorage) storeDirectoryRecord(ctx context.Context, key string, ts time.Time, baseIsDir bool) error {

	// Extract parent directory and base (file) names from key
	dir, base := rs.splitDirectoryKey(key, baseIsDir)
	// Reached the top-level directory
	if dir == "." {
		return nil
	}

	// Insert "base" value into Set "dir"
	success, err := rs.client.ZAdd(ctx, dir, redis.Z{Score: float64(ts.Unix()), Member: base}).Result()
	if err != nil {
		return fmt.Errorf("Unable to add %s to Set %s: %v", base, dir, err)
	}

	// Non-zero success means base was added to the set (not already there)
	if success > 0 {
		// recursively create parent directory until already
		// created (success == 0) or top level reached
		rs.storeDirectoryRecord(ctx, dir, ts, true)
	}

	return nil
}

func (rs RedisStorage) deleteDirectoryRecord(ctx context.Context, key string, baseIsDir bool) error {

	dir, base := rs.splitDirectoryKey(key, baseIsDir)
	// Reached the top-level directory
	if dir == "." {
		return nil
	}

	// Remove "base" value from Set "dir"
	if err := rs.client.ZRem(ctx, dir, base).Err(); err != nil {
		return fmt.Errorf("Unable to remove %s from Set %s: %v", base, dir, err)
	}

	// Check if Set "dir" still exists (removing the last item deletes the set)
	if exists := rs.client.Exists(ctx, dir).Val(); exists == 0 {
		// Recursively delete parent directory until parent
		// is not empty (exists > 0) or top level reached
		rs.deleteDirectoryRecord(ctx, dir, true)
	}

	return nil
}

func (rs RedisStorage) splitDirectoryKey(key string, baseIsDir bool) (string, string) {

	dir := path.Dir(key)
	base := path.Base(key)

	// Append slash to indicate directory
	if baseIsDir {
		base = base + "/"
	}

	return dir, base
}

func (rs RedisStorage) String() string {
	redacted := `REDACTED`
	if rs.Password != "" {
		rs.Password = redacted
	}
	if rs.EncryptionKey != "" {
		rs.EncryptionKey = redacted
	}
	strVal, _ := json.Marshal(rs)
	return string(strVal)
}
