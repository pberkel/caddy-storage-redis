# Redis Storage module for Caddy / Certmagic

This is comprehensive rewrite of the [gamalan/caddy-tlsredis](https://github.com/gamalan/caddy-tlsredis) Redis storage plugin for Caddy.  It fixes some issues with configuration option parsing, introduces the ability to compress stored data and implements a new set-based indexing approach which provides more efficient directory traversal.

Previous configuration options are generally compatible except for `CADDY_CLUSTERING_REDIS_*` environment variables, which have been removed.  To configure this Redis Storage module using environment variables, see the example configuration below.

Upgrading to this module from [gamalan/caddy-tlsredis](https://github.com/gamalan/caddy-tlsredis) will require an [export storage](https://caddyserver.com/docs/command-line#caddy-storage) from the previous installation then [import storage](https://caddyserver.com/docs/command-line#caddy-storage) into a new Caddy server instance running this module.  The default `key_prefix` has been changed from `caddytls` to `caddy` to provide a simpler migration path so keys stored by the [gamalan/caddy-tlsredis](https://github.com/gamalan/caddy-tlsredis) plugin and this module can co-exist in the same Redis database.

The current implementation only supports connecting to a single Redis instance but future support for Redis Cluster and Redis Sentinal is planned.

The plugin uses the latest version of the [go-redis/redis](https://github.com/go-redis/redis) client and [redislock](https://github.com/bsm/redislock) for the locking mechanism. See [distlock](https://redis.io/topics/distlock) for more information on the lock algorithm.

## Configuration

Enable Redis storage in Caddy by specifying the module configuration in the Caddyfile:
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
Note that you can configure `host` and `port` values (or accept the defaults) OR specify an `address` value, which overrides `host` and `port` values if set.

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

:443 {

}
```

JSON configuration example
```
{
    "admin": {
        "listen": "0.0.0.0:2019"
    },
    "storage": {
        "address": "127.0.0.1:6379",
        "compression": true,
        "db": 1,
        "host": "127.0.0.1",
        "key_prefix": "caddy",
        "module": "redis",
        "password": "",
        "port": "6379",
        "timeout": 5,
        "encryption_key": "1aedfs5kcM8lOZO3BDDMuwC23croDwRr",
        "tls_enabled": true,
        "tls_insecure": false
    }
}
```

## TODO

- Redis Cluster and Sentinel support (which may first require an update the distlock implementation)
