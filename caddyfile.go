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
			return New()
		},
	}
}

func (rs *RedisStorage) CertMagicStorage() (certmagic.Storage, error) {
	return rs, nil
}

func (rs *RedisStorage) UnmarshalCaddyfile(d *caddyfile.Dispenser) error {

	repl := caddy.NewReplacer()

	for d.Next() {

		// Optional Redis client type either "cluster" or "failover"
		if d.NextArg() {
			val := d.Val()
			if val == "cluster" || val == "failover" {
				rs.ClientType = val
			} else {
				return d.ArgErr()
			}
		}

		for nesting := d.Nesting(); d.NextBlock(nesting); {

			configKey := d.Val()
			var configVal []string

			if d.NextArg() {
				// configuration item with single parameter
				configVal = append(configVal, repl.ReplaceAll(d.Val(), ""))
			} else {
				// configuration item with nested parameter list
				for nesting := d.Nesting(); d.NextBlock(nesting); {
					configVal = append(configVal, repl.ReplaceAll(d.Val(), ""))
				}
			}

			switch configKey {
			case "address":
				for _, val := range configVal {
					host, port, err := net.SplitHostPort(val)
					if err != nil {
						return d.Errf("invalid address: %s", val)
					}
					rs.Address = append(rs.Address, net.JoinHostPort(host, port))
				}
			case "host":
				// Reset Host to override defaults
				rs.Host = []string{}
				for _, val := range configVal {
					addr := net.ParseIP(val)
					_, err := net.LookupHost(val)
					if addr == nil && err != nil {
						return d.Errf("invalid host value: %s", val)
					}
					rs.Host = append(rs.Host, val)
				}
			case "port":
				// Reset Port to override defaults
				rs.Port = []string{}
				for _, val := range configVal {
					_, err := strconv.Atoi(val)
					if err != nil {
						return d.Errf("invalid port value: %s", val)
					}
					rs.Port = append(rs.Port, val)
				}
			case "db":
				dbParse, err := strconv.Atoi(configVal[0])
				if err != nil {
					return d.Errf("invalid db value: %s", configVal[0])
				}
				rs.DB = dbParse
			case "timeout":
				timeParse, err := strconv.Atoi(configVal[0])
				if err != nil || timeParse < 0 {
					return d.Errf("invalid timeout value: %s", configVal[0])
				}
				rs.Timeout = configVal[0]
			case "username":
				if configVal[0] != "" {
					rs.Username = configVal[0]
				}
			case "password":
				if configVal[0] != "" {
					rs.Password = configVal[0]
				}
			case "master_name":
				if configVal[0] != "" {
					rs.MasterName = configVal[0]
				}
			case "key_prefix":
				if configVal[0] != "" {
					rs.KeyPrefix = configVal[0]
				}
			case "encryption_key", "aes_key":
				// Encryption_key length must be at least 32 characters
				if len(configVal[0]) < 32 {
					return d.Errf("invalid length for 'encryption_key', must contain at least 32 bytes: %s", configVal[0])
				}
				// Truncate keys that are too long
				if len(configVal[0]) > 32 {
					rs.EncryptionKey = configVal[0][:32]
				} else {
					rs.EncryptionKey = configVal[0]
				}
			case "compression":
				Compression, err := strconv.ParseBool(configVal[0])
				if err != nil {
					return d.Errf("invalid boolean value for 'compression': %s", configVal[0])
				}
				rs.Compression = Compression
			case "tls_enabled":
				TlsEnabledParse, err := strconv.ParseBool(configVal[0])
				if err != nil {
					return d.Errf("invalid boolean value for 'tls_enabled': %s", configVal[0])
				}
				rs.TlsEnabled = TlsEnabledParse
			case "tls_insecure":
				tlsInsecureParse, err := strconv.ParseBool(configVal[0])
				if err != nil {
					return d.Errf("invalid boolean value for 'tls_insecure': %s", configVal[0])
				}
				rs.TlsInsecure = tlsInsecureParse
			case "route_by_latency":
				routeByLatency, err := strconv.ParseBool(configVal[0])
				if err != nil {
					return d.Errf("invalid boolean value for 'route_by_latency': %s", configVal[0])
				}
				rs.RouteByLatency = routeByLatency

			case "route_randomly":
				routeRandomly, err := strconv.ParseBool(configVal[0])
				if err != nil {
					return d.Errf("invalid boolean value for 'route_randomly': %s", configVal[0])
				}
				rs.RouteRandomly = routeRandomly
			}
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
		rs.logger.Infof("Provision Redis %s storage using address %v", rs.ClientType, rs.Address)
	}

	return err
}

func (rs *RedisStorage) finalizeConfiguration(ctx context.Context) error {

	// Construct Address from Host and Port if not explicitly provided
	if len(rs.Address) == 0 {

		var maxAddrs int
		var host, port string

		maxHosts := len(rs.Host)
		maxPorts := len(rs.Port)

		// Determine max number of addresses
		if maxHosts > maxPorts {
			maxAddrs = maxHosts
		} else {
			maxAddrs = maxPorts
		}

		for i := 0; i < maxAddrs; i++ {
			if i < maxHosts {
				host = rs.Host[i]
			}
			if i < maxPorts {
				port = rs.Port[i]
			}
			rs.Address = append(rs.Address, net.JoinHostPort(host, port))
		}
		// Clear host and port values
		rs.Host = []string{}
		rs.Port = []string{}
	}

	return rs.initRedisClient(ctx)
}

func (rs *RedisStorage) Cleanup() error {
	// Close the Redis connection
	if rs.client != nil {
		rs.client.Close()
	}

	return nil
}

// Interface guards
var (
	_ caddy.CleanerUpper     = (*RedisStorage)(nil)
	_ caddy.Provisioner      = (*RedisStorage)(nil)
	_ caddy.StorageConverter = (*RedisStorage)(nil)
	_ caddyfile.Unmarshaler  = (*RedisStorage)(nil)
)
