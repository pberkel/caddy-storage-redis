# v1.5.0 (2025-12-11)

This version adds support for authenticating with Redis Sentinel servers by 
introducing a new configuration parameter named _sentinel_password_. This allows 
users to specify a separate password for Sentinel authentication, improving 
compatibility with secured Redis Sentinel setups.

# v1.4.0 (2024-11-12)

- Expose Redis Go client to allow other modules to access it.
- Minor updates to the Redis Sentinel documentation for clarity.
- Additional checks in caddyfile parsing code to ensure keys do not have empty values.

# v1.3.0 (2024-07-03)

Updated documentation and project dependancies only. No functional changes included.

# v1.2.0 (2024-04-03)

### Move placeholder validation to Provision to support runtime substitution

Caddy placeholders like _{env.VALUE}_ should not be evaluated during Caddyfile 
parsing. The syntax _{env.VALUE}_ is for runtime environmental variables, and so 
should be preserved as strings in configuration. The syntax _{$VALUE}_ will 
result in environmental variables substituted at Caddyfile parse time, which 
is already performed by the Caddyfile parser.

# v1.1.0 (2023-12-19)

Add options for TLS server certs as either PEM string or path to PEM cert file.

Allow configuring the trust store used to verify connections to Redis.
This is useful when working with something like GCP Memorystore for
Redis ([1]), which will issue a self-signed cert for managed Redis
instances.

[1]: https://cloud.google.com/memorystore/docs/redis

# v1.0.0 (2025-11-26)

First official public release of the Caddy Storage Redis module.
