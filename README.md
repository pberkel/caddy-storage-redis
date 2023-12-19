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

### Enabling TLS

TLS is disabled by default, and if enabled, accepts any server certificate by default. If TLS is and certificate verification are enabled as in the following example, then the system trust store will be used to validate the server certificate.
```
{
    storage redis {
        host 127.0.0.01
        port 6379
        tls_enabled true
        tls_insecure false
    }
}
```
You can also use the `tls_server_certs_pem` option to provide one or more PEM encoded certificates to trust:
```
{
    storage redis {
        host 127.0.0.01
        port 6379
        tls_enabled true
        tls_insecure false
        tls_server_certs_pem <<CERTIFICATES
        -----BEGIN CERTIFICATE-----
        MIIDnTCCAoWgAwIBAgIBADANBgkqhkiG9w0BAQsFADCBhTEtMCsGA1UELhMkMzZk
        MWE2MjgtNGZjNi00ZTRkLWJiNDMtZDhlMGNhN2I1OTRiMTEwLwYDVQQDEyhHb29n
        bGUgQ2xvdWQgTWVtb3J5c3RvcmUgUmVkaXMgU2VydmVyIENBMRQwEgYDVQQKEwtH
        b29nbGUsIEluYzELMAkGA1UEBhMCVVMwHhcNMjMxMjE1MjM0MDMyWhcNMzMxMjEy
        MjM0MTMyWjCBhTEtMCsGA1UELhMkMzZkMWE2MjgtNGZjNi00ZTRkLWJiNDMtZDhl
        MGNhN2I1OTRiMTEwLwYDVQQDEyhHb29nbGUgQ2xvdWQgTWVtb3J5c3RvcmUgUmVk
        aXMgU2VydmVyIENBMRQwEgYDVQQKEwtHb29nbGUsIEluYzELMAkGA1UEBhMCVVMw
        ggEiMA0GCSqGSIb3DQEBAQUAA4IBDwAwggEKAoIBAQCF54WBXJ8kTAj7e843XriG
        oXntUoQBP+TdmzBdgW/t9xqi9di7I6zbyl86x+aOENU8xgHQZQxQ/uE0cnJeaMuH
        H7smyiSn77IP+JL3icDk8a8QIJxYmv3ze47a5ZbfJ4VPXYk0Kh/1HXMDMguS2e+a
        PdjhCVZSB1rwgaH6nAIjmoJxdKSiNolm4xeuZPXwzvsuZZXhc+HIOiZMhckxnZfD
        tZsSYZhg0TgswG1DWP+Nq79Z8SSb+uXHPdOEI2w1YKpcZyh5WuGcarMswRh8E3Kf
        UC+9JLot5NBZ+oAKqcQ7R55Wxd+8CI0paPqaccbJgXMIA2pSEhiqNMEYSA/9QtV3
        AgMBAAGjFjAUMBIGA1UdEwEB/wQIMAYBAf8CAQAwDQYJKoZIhvcNAQELBQADggEB
        ABO7LLHzvGkz/IMAEkEyJlQAOrKZD5qC4jTuICQqm9xV17Ql2SLEdKZzAFrEDLJR
        by0dWrPconQG7XqLgb22RceBVKzEGsmObI7LZQLo69MUYI4TcRDgAXeng34yUBRo
        njv+WFAQWNUym4WhUeRceyyOWmzhlC0/zOJPufmVBk6QNmjTfXG2ISCeZhFM0rEb
        C8amwlD9V3EXFjTAEoYs+9Uv1iYDjlMtMrygrrCFTe61Kcgtzp1jsIjfYmTCyt5S
        WVCmGu+wdiPFL9/N0peb5/ORGrdEg4n+a+gCHV9LGVfUcFCyfR42+4FunKwE/OMl
        PaAxpc/KB4nwPitpbsWL8Nw=
        -----END CERTIFICATE-----
        -----BEGIN CERTIFICATE-----
        <another certificate here>
        -----END CERTIFICATE-----
        CERTIFICATES
    }
}
```
If you prefer not to put certificates in your Caddyfile, you can also put the series of PEM certificates into a file and use `tls_server_certs_path` to point Caddy at it.

## Maintenance

This module has been architected to maintain a hierarchical index of storage items using Redis Sorted Sets to optimize directory listing operations typically used by Caddy.  It is possible for this index structure to become corrupted in the event of an unexpected system crash or loss of power.  If you suspect your Caddy storage has been corrupted, it is possible to repair this index structure from the command line by issuing the following command:

```
caddy redis repair --config /path/to/Caddyfile
```

Note that the config parameter is optional (but recommended); if not specified Caddy look for a configuration file named "Caddyfile" in the current working directory.
