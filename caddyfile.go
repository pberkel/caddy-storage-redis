package storageredis

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"reflect"
	"strconv"

	"github.com/spf13/cobra"

	"github.com/caddyserver/caddy/v2"
	"github.com/caddyserver/caddy/v2/caddyconfig/caddyfile"
	caddycmd "github.com/caddyserver/caddy/v2/cmd"
	"github.com/caddyserver/certmagic"
)

func init() {
	caddy.RegisterModule(RedisStorage{})
	caddycmd.RegisterCommand(caddycmd.Command{
		Name:  "redis",
		Short: "Commands for working with the Caddy Redis Storage module",
		CobraFunc: func(cmd *cobra.Command) {
			rebuildCmd := &cobra.Command{
				Use:   "repair --config <path>",
				Short: "Repair the Redis Storage directory index tree",
				RunE:  caddycmd.WrapCommandFuncForCobra(cmdRedisStorageRepair),
			}
			rebuildCmd.Flags().StringP("config", "c", "", "Caddy configuration file (optional)")
			cmd.AddCommand(rebuildCmd)
		},
	})
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
				configVal = append(configVal, d.Val())
			} else {
				// configuration item with nested parameter list
				for nesting := d.Nesting(); d.NextBlock(nesting); {
					configVal = append(configVal, d.Val())
				}
			}
			// There are no valid configurations where configVal slice is empty
			if len(configVal) == 0 {
				return d.Errf("no value supplied for configuraton key '%s'", configKey)
			}

			switch configKey {
			case "address":
				rs.Address = configVal
			case "host":
				rs.Host = configVal
			case "port":
				rs.Port = configVal
			case "db":
				dbParse, err := strconv.Atoi(configVal[0])
				if err != nil {
					return d.Errf("invalid db value: %s", configVal[0])
				}
				rs.DB = dbParse
			case "timeout":
				rs.Timeout = configVal[0]
			case "username":
				if configVal[0] != "" {
					rs.Username = configVal[0]
				}
			case "password":
				if configVal[0] != "" {
					rs.Password = configVal[0]
				}
			case "sentinel_password":
				if configVal[0] != "" {
					rs.SentinelPassword = configVal[0]
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
				rs.EncryptionKey = configVal[0]
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
			case "tls_server_certs_pem":
				if configVal[0] != "" {
					rs.TlsServerCertsPEM = configVal[0]
				}
			case "tls_server_certs_path":
				if configVal[0] != "" {
					rs.TlsServerCertsPath = configVal[0]
				}
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

	repl := caddy.NewReplacer()

	for idx, v := range rs.Address {
		v = repl.ReplaceAll(v, "")
		host, port, err := net.SplitHostPort(v)
		if err != nil {
			return fmt.Errorf("invalid address: %s", v)
		}
		rs.Address[idx] = net.JoinHostPort(host, port)
	}
	for idx, v := range rs.Host {
		v = repl.ReplaceAll(v, defaultHost)
		addr := net.ParseIP(v)
		_, err := net.LookupHost(v)
		if addr == nil && err != nil {
			return fmt.Errorf("invalid host value: %s", v)
		}
		rs.Host[idx] = v
	}
	for idx, v := range rs.Port {
		v = repl.ReplaceAll(v, defaultPort)
		_, err := strconv.Atoi(v)
		if err != nil {
			return fmt.Errorf("invalid port value: %s", v)
		}
		rs.Port[idx] = v
	}
	if rs.Timeout = repl.ReplaceAll(rs.Timeout, ""); rs.Timeout != "" {
		timeParse, err := strconv.Atoi(rs.Timeout)
		if err != nil || timeParse < 0 {
			return fmt.Errorf("invalid timeout value: %s", rs.Timeout)
		}
	}
	rs.MasterName = repl.ReplaceAll(rs.MasterName, "")
	rs.Username = repl.ReplaceAll(rs.Username, "")
	rs.Password = repl.ReplaceAll(rs.Password, "")
	rs.KeyPrefix = repl.ReplaceAll(rs.KeyPrefix, defaultKeyPrefix)

	if len(rs.EncryptionKey) > 0 {
		rs.EncryptionKey = repl.ReplaceAll(rs.EncryptionKey, "")
		// Encryption_key length must be at least 32 characters
		if len(rs.EncryptionKey) < 32 {
			return fmt.Errorf("invalid length for 'encryption_key', must contain at least 32 bytes: %s", rs.EncryptionKey)
		}
		// Truncate keys that are too long
		if len(rs.EncryptionKey) > 32 {
			rs.EncryptionKey = rs.EncryptionKey[:32]
		}
	}

	rs.TlsServerCertsPEM = repl.ReplaceAll(rs.TlsServerCertsPEM, "")
	rs.TlsServerCertsPath = repl.ReplaceAll(rs.TlsServerCertsPath, "")

	// TODO: these are non-string fields so they can't easily be substituted at runtime :(
	// rs.DB
	// rs.Compression
	// rs.TlsEnabled
	// rs.TlsInsecure
	// rs.RouteByLatency
	// rs.RouteRandomly

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

type storageConfig struct {
	StorageRaw json.RawMessage `json:"storage,omitempty" caddy:"namespace=caddy.storage inline_key=module"`
}

func cmdRedisStorageRepair(fl caddycmd.Flags) (int, error) {

	configFile := fl.String("config")

	// Load configuration file (if not specified, will look in usual locations)
	cfg, _, _, err := caddycmd.LoadConfig(configFile, "")
	if err != nil {
		return caddy.ExitCodeFailedStartup, fmt.Errorf("Unable to load config file: %v", err)
	}

	// Unmarshall the storage configuration into a temporary struct
	var storeCfg storageConfig
	if err := json.Unmarshal(cfg, &storeCfg); err != nil || storeCfg.StorageRaw == nil {
		return caddy.ExitCodeFailedStartup, fmt.Errorf("Unable to unmarshal configuration: %v", err)
	}

	ctx, cancel := caddy.NewContext(caddy.Context{Context: context.Background()})
	defer cancel()

	// Load module
	module, err := ctx.LoadModule(&storeCfg, "StorageRaw")
	if err != nil {
		return caddy.ExitCodeFailedStartup, err
	}
	// Ensure loaded module is the correct type
	if reflect.TypeOf(module) != reflect.TypeOf(&RedisStorage{}) {
		return caddy.ExitCodeFailedStartup, fmt.Errorf("Loaded storage module does not support Redis")
	}

	rs := module.(*RedisStorage)
	if err := rs.Repair(ctx, ""); err != nil {
		return caddy.ExitCodeFailedStartup, err
	}

	return caddy.ExitCodeSuccess, nil
}

// Interface guards
var (
	_ caddy.CleanerUpper     = (*RedisStorage)(nil)
	_ caddy.Provisioner      = (*RedisStorage)(nil)
	_ caddy.StorageConverter = (*RedisStorage)(nil)
	_ caddyfile.Unmarshaler  = (*RedisStorage)(nil)
)
