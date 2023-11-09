# Redis Storage module for Caddy / Certmagic

This is comprehensive rewrite of the [gamalan/caddy-tlsredis](https://github.com/gamalan/caddy-tlsredis) Redis storage plugin for Caddy.  Some highlights of this new version:

* Fixes some logic issues with configuration parsing
* Introduces a new storage compression option
* Features a Sorted Set indexing algorithm for more efficient directory traversal
* Implements support for Redis Cluster and Sentinal / Failover servers

The plugin uses the latest version of the [go-redis/redis](https://github.com/go-redis/redis) client and [redislock](https://github.com/bsm/redislock) for the locking mechanism. See [distlock](https://redis.io/topics/distlock) for more information on the lock algorithm.


## Upgrading

Previous configuration options are generally compatible except for `CADDY_CLUSTERING_REDIS_*` environment variables, which have been removed.  To configure this Redis Storage module using environment variables, see the example configuration below.

Upgrading to this module from [gamalan/caddy-tlsredis](https://github.com/gamalan/caddy-tlsredis) will require an [export storage](https://caddyserver.com/docs/command-line#caddy-storage) from the previous installation then [import storage](https://caddyserver.com/docs/command-line#caddy-storage) into a new Caddy server instance running this module.  The default `key_prefix` has been changed from `caddytls` to `caddy` to provide a simpler migration path so keys stored by the [gamalan/caddy-tlsredis](https://github.com/gamalan/caddy-tlsredis) plugin and this module can co-exist in the same Redis database.

## Configuration

### Simple mode (Standalone)

Enable Redis storage for Caddy by specifying the module configuration in the Caddyfile:
```
{
    // All values are optional, below are the defaults
    storage redis {
        host           127.0.0.1
        port           6379
        address        127.0.0.1:6379 // derived from host and port values if not explicitly set
        username       ""
        password       ""
        db             0
        timeout        5
        key_prefix     "caddy"
        encryption_key ""    // default no encryption; enable by specifying a secret key containing 32 characters (longer keys will be truncated)
        compression    false // default no compression; if set to true, stored values are compressed using "compress/flate"
        tls_enabled    false
        tls_insecure   true
    }
}

:443 {

}
```
Note that `host` and `port` values can be configured (or accept the defaults) OR an `address` value can be specified, which will override the `host` and `port` values.

The module supports [environment variable substitution](https://caddyserver.com/docs/caddyfile/concepts#environment-variables) within Caddyfile parameters:
```
{
    storage redis {
        username       "{env.REDIS_USERNAME}"
        password       "{env.REDIS_PASSWORD}"
        encryption_key "{env.REDIS_ENCRYPTION_KEY}"
        compression    true
    }
}
```

### Cluster mode

Connect to a Redis Cluster by specifying a flag before the main configuration block or by configuring more than one Redis host / address:
```
{
    storage redis cluster {
        address {
            redis-cluster-001.example.com:6379
            redis-cluster-002.example.com:6379
            redis-cluster-003.example.com:6379
        }
    }
}
```

It is also possible to configure the cluster by specifying a single configuration endpoint:
```
{
    storage redis cluster {
        address clustercfg.redis-cluster.example.com:6379
    }
}
```

Parameters `address`, `host`, and `port` all accept either single or multiple input values. A cluster of Redis servers all listening on the same port can be configured simply:
```
{
    storage redis cluster {
        host {
            redis-cluster-001.example.com
            redis-cluster-002.example.com
            redis-cluster-003.example.com
        }
        port 6379
        route_by_latency false
        route_randomly false
    }
}
```
Two optional boolean cluster parameters `route_by_latency` and `route_randomly` are supported.  Either option can be enabled by setting the value to `true` (Default is false)

### Failover mode (Sentinal)

Connecting to Redis servers managed by Sentinal requires both the `failover` flag and `master_name` to be set:
```
{
    storage redis failover {
        address {
            redis-sentinal-001.example.com:6379
            redis-sentinal-002.example.com:6379
            redis-sentinal-003.example.com:6379
        }
        master_name redis-sentinal-001
    }
}
```
Failover mode also supports the `route_by_latency` and `route_randomly` cluster configuration parameters.
