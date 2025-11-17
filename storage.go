package storageredis

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/bsm/redislock"
	"github.com/caddyserver/certmagic"
	"github.com/redis/go-redis/v9"
	"go.uber.org/zap"
)

const (
	// Redis client type
	defaultClientType = "simple"

	// Redis server host
	defaultHost = "127.0.0.1"

	// Redis server port
	defaultPort = "6379"

	// Redis server database
	defaultDb = 0

	// Prepended to every Redis key
	defaultKeyPrefix = "caddy"

	// Compress values before storing
	defaultCompression = false

	// Connect to Redis via TLS
	defaultTLS = false

	// Do not verify TLS cerficate
	defaultTLSInsecure = true

	// Routing option for cluster client
	defaultRouteByLatency = false

	// Routing option for cluster client
	defaultRouteRandomly = false

	// Redis lock time-to-live
	lockTTL = 5 * time.Second

	// Delay between attempts to obtain Lock
	lockPollInterval = 1 * time.Second

	// How frequently the Lock's TTL should be updated
	lockRefreshInterval = 3 * time.Second
)

// RedisStorage implements a Caddy storage backend for Redis
// It supports Single (Standalone), Cluster, or Sentinal (Failover) Redis server configurations.
type RedisStorage struct {
	// ClientType specifies the Redis client type. Valid values are "cluster" or "failover"
	ClientType    string   `json:"client_type"`
	// Address The full address of the Redis server. Example: "127.0.0.1:6379"
	// If not defined, will be generated from Host and Port parameters.
	Address       []string `json:"address"`
	// Host The Redis server hostname or IP address. Default: "127.0.0.1"
	Host          []string `json:"host"`
	// Host The Redis server port number. Default: "6379"
	Port          []string `json:"port"`
	// DB The Redis server database number. Default: 0
	DB            int      `json:"db"`
	// Timeout The Redis server timeout in seconds. Default: 5
	Timeout       string   `json:"timeout"`
	// Username The username for authenticating with the Redis server. Default: "" (No authentication)
	Username      string   `json:"username"`
	// Password The password for authenticating with the Redis server. Default: "" (No authentication)
	Password      string   `json:"password"`
	// PasswordSentinal Optional password needed if redis sentinals requires authentication.
	PasswordSentinal string `json:"password_sentinal"`
	// MasterName Only required when connecting to Redis via Sentinal (Failover mode). Default ""
	MasterName    string   `json:"master_name"`
	// KeyPrefix A string prefix that is appended to Redis keys. Default: "caddy"
	// Useful when the Redis server is used by multiple applications.
	KeyPrefix     string   `json:"key_prefix"`
	// EncryptionKey A key string used to symmetrically encrypt and decrypt data stored in Redis.
	// The key must be exactly 32 characters, longer values will be truncated. Default: "" (No encryption)
	EncryptionKey string   `json:"encryption_key"`
	// Compression Specifies whether values should be compressed before storing in Redis. Default: false
	Compression   bool     `json:"compression"`
	// TlsEnabled controls whether TLS will be used to connect to the Redis
	// server. False by default.
	TlsEnabled bool `json:"tls_enabled"`
	// TlsInsecure controls whether the client will verify the server
	// certificate. See `InsecureSkipVerify` in `tls.Config` for details. True
	// by default.
	// https://pkg.go.dev/crypto/tls#Config
	TlsInsecure bool `json:"tls_insecure"`
	// TlsServerCertsPEM is a series of PEM encoded certificates that will be
	// used by the client to validate trust in the Redis server's certificate
	// instead of the system trust store. May not be specified alongside
	// `TlsServerCertsPath`. See `x509.CertPool.AppendCertsFromPem` for details.
	// https://pkg.go.dev/crypto/x509#CertPool.AppendCertsFromPEM
	TlsServerCertsPEM string `json:"tls_server_certs_pem"`
	// TlsServerCertsPath is the path to a file containing a series of PEM
	// encoded certificates that will be used by the client to validate trust in
	// the Redis server's certificate instead of the system trust store. May not
	// be specified alongside `TlsServerCertsPem`. See
	// `x509.CertPool.AppendCertsFromPem` for details.
	// https://pkg.go.dev/crypto/x509#CertPool.AppendCertsFromPEM
	TlsServerCertsPath string `json:"tls_server_certs_path"`
	// RouteByLatency Route commands by latency, only used in Cluster mode. Default: false
	RouteByLatency     bool   `json:"route_by_latency"`
	// RouteRandomly Route commands randomly, only used in Cluster mode. Default: false
	RouteRandomly      bool   `json:"route_randomly"`

	client redis.UniversalClient
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
func New() *RedisStorage {

	rs := RedisStorage{
		ClientType:  defaultClientType,
		Host:        []string{defaultHost},
		Port:        []string{defaultPort},
		DB:          defaultDb,
		KeyPrefix:   defaultKeyPrefix,
		Compression: defaultCompression,
		TlsEnabled:  defaultTLS,
		TlsInsecure: defaultTLSInsecure,
	}
	return &rs
}

// Initilalize Redis client and locker
func (rs *RedisStorage) initRedisClient(ctx context.Context) error {

	// Configure options for all client types
	clientOpts := redis.UniversalOptions{
		Addrs:      rs.Address,
		MasterName: rs.MasterName,
		Username:   rs.Username,
		Password:   rs.Password,
		DB:         rs.DB,
	}

	// Configure timeout values if defined
	if rs.Timeout != "" {
		// Was already sanity-checked in UnmarshalCaddyfile
		timeout, _ := strconv.Atoi(rs.Timeout)
		clientOpts.DialTimeout = time.Duration(timeout) * time.Second
		clientOpts.ReadTimeout = time.Duration(timeout) * time.Second
		clientOpts.WriteTimeout = time.Duration(timeout) * time.Second
	}

	// Configure cluster routing options
	if rs.RouteByLatency || rs.RouteRandomly {
		clientOpts.RouteByLatency = rs.RouteByLatency
		clientOpts.RouteRandomly = rs.RouteRandomly
	}

	// Configure TLS support if enabled
	if rs.TlsEnabled {
		clientOpts.TLSConfig = &tls.Config{
			InsecureSkipVerify: rs.TlsInsecure,
		}

		if len(rs.TlsServerCertsPEM) > 0 && len(rs.TlsServerCertsPath) > 0 {
			return fmt.Errorf("Cannot specify TlsServerCertsPEM alongside TlsServerCertsPath")
		}

		if len(rs.TlsServerCertsPEM) > 0 || len(rs.TlsServerCertsPath) > 0 {
			certPool := x509.NewCertPool()
			pem := []byte(rs.TlsServerCertsPEM)

			if len(rs.TlsServerCertsPath) > 0 {
				var err error
				pem, err = os.ReadFile(rs.TlsServerCertsPath)
				if err != nil {
					return fmt.Errorf("Failed to load PEM server certs from file %s: %v", rs.TlsServerCertsPath, err)
				}
			}

			if !certPool.AppendCertsFromPEM(pem) {
				return fmt.Errorf("Failed to load PEM server certs")
			}

			clientOpts.TLSConfig.RootCAs = certPool
		}
	}

	// Create appropriate Redis client type
	if rs.ClientType == "failover" && clientOpts.MasterName != "" {

		if rs.PasswordSentinal != "" {
			clientOpts.SentinelPassword = rs.PasswordSentinal
		}

		// Create new Redis Failover Cluster client
		clusterClient := redis.NewFailoverClusterClient(clientOpts.Failover())

		// Test connection to the Redis cluster
		err := clusterClient.ForEachShard(ctx, func(ctx context.Context, shard *redis.Client) error {
			return shard.Ping(ctx).Err()
		})
		if err != nil {
			return err
		}
		rs.client = clusterClient

	} else if rs.ClientType == "cluster" || len(clientOpts.Addrs) > 1 {

		// Create new Redis Cluster client
		clusterClient := redis.NewClusterClient(clientOpts.Cluster())

		// Test connection to the Redis cluster
		err := clusterClient.ForEachShard(ctx, func(ctx context.Context, shard *redis.Client) error {
			return shard.Ping(ctx).Err()
		})
		if err != nil {
			return err
		}
		rs.client = clusterClient

	} else {

		// Create new Redis simple standalone client
		rs.client = redis.NewClient(clientOpts.Simple())

		// Test connection to the Redis server
		err := rs.client.Ping(ctx).Err()
		if err != nil {
			return err
		}
	}

	// Create new redislock client
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
	score := float64(sd.Modified.Unix())
	if err := rs.storeDirectoryRecord(ctx, prefixedKey, score, false, false); err != nil {
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
	var currKey = rs.prefixKey(dir)

	// Obtain range of all direct children stored in the Sorted Set
	keys, err := rs.client.ZRange(ctx, currKey, 0, -1).Result()
	if err != nil {
		return keyList, fmt.Errorf("Unable to get range on sorted set '%s': %v", currKey, err)
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

func (rs *RedisStorage) Repair(ctx context.Context, dir string) error {

	var currKey = rs.prefixKey(dir)

	// Perform recursive full key scan only from the root directory
	if dir == "" {

		var pointer uint64 = 0
		var scanCount int64 = 500

		for {
			// Scan for keys matching the search query and iterate until all found
			keys, nextPointer, err := rs.client.Scan(ctx, pointer, currKey+"*", scanCount).Result()
			if err != nil {
				return fmt.Errorf("Unable to scan path %s: %v", currKey, err)
			}

			// Iterate over returned keys
			for _, key := range keys {
				// Proceed only if key type is regular string value
				keyType := rs.client.Type(ctx, key).Val()
				if keyType != "string" {
					continue
				}

				// Load the Storage Data struct to obtain modified time
				trimmedKey := rs.trimKey(key)
				sd, err := rs.loadStorageData(ctx, trimmedKey)
				if err != nil {
					rs.logger.Infof("Unable to load storage data for key '%s'", trimmedKey)
					continue
				}

				// Repair directory structure set for current key
				score := float64(sd.Modified.Unix())
				if err := rs.storeDirectoryRecord(ctx, key, score, true, false); err != nil {
					return fmt.Errorf("Unable to repair directory index for key '%s'", trimmedKey)
				}
			}

			// End of results reached
			if nextPointer == 0 {
				break
			}
			pointer = nextPointer
		}
	}

	// Obtain range of all direct children stored in the Sorted Set
	keys, err := rs.client.ZRange(ctx, currKey, 0, -1).Result()
	if err != nil {
		return fmt.Errorf("Unable to get range on sorted set '%s': %v", currKey, err)
	}

	// Iterate over each child key
	for _, k := range keys {
		// Directory keys will have a "/" suffix
		trimmedKey := strings.TrimSuffix(k, "/")

		// Reconstruct the full path of child key
		fullPathKey := path.Join(dir, trimmedKey)

		// Remove key from set if it does not exist
		if !rs.Exists(ctx, fullPathKey) {
			rs.client.ZRem(ctx, currKey, k)
			rs.logger.Infof("Removed non-existant record '%s' from directory '%s'", k, currKey)
			continue
		}

		// If current key is a directory
		if k != trimmedKey {
			// Recursively traverse all child directories
			if err := rs.Repair(ctx, fullPathKey); err != nil {
				return err
			}
		}
	}

	return nil
}

func (rs *RedisStorage) trimKey(key string) string {
	return strings.TrimPrefix(strings.TrimPrefix(key, rs.KeyPrefix), "/")
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

// Store directory index in Redis ZSet structure for fast and efficient travseral in List()
func (rs RedisStorage) storeDirectoryRecord(ctx context.Context, key string, score float64, repair, baseIsDir bool) error {

	// Extract parent directory and base (file) names from key
	dir, base := rs.splitDirectoryKey(key, baseIsDir)
	// Reached the top-level directory
	if dir == "." {
		return nil
	}

	// Insert "base" value into Set "dir"
	success, err := rs.client.ZAdd(ctx, dir, redis.Z{Score: score, Member: base}).Result()
	if err != nil {
		return fmt.Errorf("Unable to add %s to Set %s: %v", base, dir, err)
	}

	// Non-zero success means base was added to the set (not already there)
	if success > 0 || repair {
		if success > 0 && repair {
			rs.logger.Infof("Repaired index for record '%s' in directory '%s'", base, dir)
		}
		// recursively create parent directory until already
		// created (success == 0) or top level reached
		rs.storeDirectoryRecord(ctx, dir, score, repair, true)
	}

	return nil
}

// Delete record from directory index Redis ZSet structure
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

// GetClient returns the Redis client initialized by this storage.
//
// This is useful for other modules that need to interact with the same Redis instance.
// The return type of GetClient is "any" for forward-compatibility new versions of go-redis.
// The returned value must usually be cast to redis.UniversalClient.
func (rs *RedisStorage) GetClient() any {
	return rs.client
}
