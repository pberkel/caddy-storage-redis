package storageredis

import (
	"context"
	"net"
	"strconv"

	"github.com/caddyserver/caddy/v2"
	"github.com/caddyserver/caddy/v2/caddyconfig/caddyfile"
	"github.com/caddyserver/certmagic"
)

func init() {
	caddy.RegisterModule(RedisStorage{})
}

func (RedisStorage) CaddyModule() caddy.ModuleInfo {
	return caddy.ModuleInfo{
		ID: "caddy.storage.redis",
		New: func() caddy.Module {
			return NewRedisStorage()
		},
	}
}

func (rs *RedisStorage) CertMagicStorage() (certmagic.Storage, error) {
	return rs, nil
}

func (rs *RedisStorage) UnmarshalCaddyfile(d *caddyfile.Dispenser) error {

	repl := caddy.NewReplacer()

	for d.Next() {

		var value string
		key := d.Val()

		if !d.Args(&value) {
			continue
		}

		// use the Caddy Replacer on all configuration options
		value = repl.ReplaceAll(value, "")

		switch key {
		case "address":
			host, port, err := net.SplitHostPort(value)
			if err != nil {
				return d.Errf("Invalid address: %s", value)
			}
			rs.Address = net.JoinHostPort(host, port)
		case "host":
			addr := net.ParseIP(value)
			_, err := net.LookupHost(value)
			if addr == nil && err != nil {
				return d.Errf("Invalid host value: %s", value)
			}
			rs.Host = value
		case "port":
			_, err := strconv.Atoi(value)
			if err != nil {
				return d.Errf("Invalid port value: %s", value)
			}
			rs.Port = value
		case "db":
			dbParse, err := strconv.Atoi(value)
			if err != nil {
				return d.Errf("Invalid db value: %s", value)
			}
			rs.DB = dbParse
		case "username":
			if value != "" {
				rs.Username = value
			}
		case "password":
			if value != "" {
				rs.Password = value
			}
		case "timeout":
			timeParse, err := strconv.Atoi(value)
			if err != nil {
				return d.Errf("Invalid timeout value: %s", value)
			}
			rs.Timeout = timeParse
		case "key_prefix":
			if value != "" {
				rs.KeyPrefix = value
			}
		case "encryption_key", "aes_key":
			// Encryption_key length must be at least 32 characters
			if len(value) < 32 {
				return d.Errf("Invalid length, 'encryption_key' must contain at least 32 bytes: %s", value)
			}
			// Truncate keys that are too long
			if len(value) > 32 {
				rs.EncryptionKey = value[:32]
			} else {
				rs.EncryptionKey = value
			}
		case "compression":
			Compression, err := strconv.ParseBool(value)
			if err != nil {
				return d.Errf("Invalid boolean value for 'compression': %s", value)
			}
			rs.Compression = Compression
		case "tls_enabled":
			TlsEnabledParse, err := strconv.ParseBool(value)
			if err != nil {
				return d.Errf("Invalid boolean value for 'tls_enabled': %s", value)
			}
			rs.TlsEnabled = TlsEnabledParse
		case "tls_insecure":
			tlsInsecureParse, err := strconv.ParseBool(value)
			if err != nil {
				return d.Errf("Invalid boolean value for 'tls_insecure': %s", value)
			}
			rs.TlsInsecure = tlsInsecureParse
		}
	}
	return nil
}

// Provision module function called by Caddy Server
func (rs *RedisStorage) Provision(ctx caddy.Context) error {

	rs.logger = ctx.Logger().Sugar()

	// Abstract this logic for testing purposes
	err := rs.finalizeConfiguration(ctx)
	if err == nil {
		rs.logger.Info("Provision Redis storage module using address " + rs.Address)
	}

	return err
}

func (rs *RedisStorage) finalizeConfiguration(ctx context.Context) error {

	// Construct Address from Host and Port if not explicitly provided
	if rs.Address == "" {
		rs.Address = net.JoinHostPort(rs.Host, rs.Port)
	}

	return rs.initRedisClient(ctx)
}

// Interface guards
var (
	_ caddy.Provisioner      = (*RedisStorage)(nil)
	_ caddy.StorageConverter = (*RedisStorage)(nil)
	_ caddyfile.Unmarshaler  = (*RedisStorage)(nil)
)
