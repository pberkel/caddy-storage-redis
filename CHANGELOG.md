# v1.8.0 (unreleased)

### New features

- **`compression` now supports zlib.** The `compression` parameter accepts a new `"zlib"` value in addition to the existing `"flate"` (raw DEFLATE). zlib adds a header and Adler-32 checksum, providing integrity verification on decompression. Flate- and zlib-compressed values can coexist in the same Redis instance without any migration — the algorithm is stored per value and selected automatically on load.
- **`compression` now supports placeholder substitution.** The parameter has been changed from a boolean to a string type, enabling runtime substitution via Caddy placeholders (e.g. `{env.REDIS_COMPRESSION}`). Legacy boolean values (`true`, `false`, `1`, `0`, etc.) continue to work unchanged, with `true` mapping to `"flate"`.
- **`db` now supports placeholder substitution.** The parameter has been changed from an integer to a string type, enabling runtime substitution via Caddy placeholders (e.g. `{env.REDIS_DB}`). Legacy integer values in JSON configs continue to work unchanged.
- **`sentinel_password` now supports placeholder substitution.** The parameter previously did not go through Caddy's replacer at provision time. It now does, consistent with `password` and other credential fields.

### Improvements

- `compression` and `db` are removed from the list of parameters that do not support runtime substitution.

# v1.7.1 (2026-03-27)

### Bug fixes

- **`failover` client type now requires `master_name`.** Previously, configuring `client_type failover` without a `master_name` silently fell back to a simple standalone client. An error is now returned at startup.
- **Unknown Caddyfile configuration keys are now rejected.** Unrecognised keys (e.g. typos such as `tls_enable` instead of `tls_enabled`) previously passed silently, leaving security-relevant options at their defaults without warning. An error is now returned at parse time.
- **`Repair()` no longer panics when called without a logger.** Logger calls in `Repair()` and `storeDirectoryRecord()` are now nil-guarded, consistent with the rest of the codebase.
- **Lock refresh goroutine improved.** The refresh no longer fires immediately on lock acquisition (the lock is already fresh). Transient Redis errors during refresh are now logged as warnings and retried on the next interval, rather than silently stopping the refresh and allowing the lock to expire.
- **Decrypt minimum-length guard tightened.** The short-ciphertext check now uses `gcm.NonceSize() + gcm.Overhead()` (28 bytes) derived from the cipher instance, replacing the weaker `aes.BlockSize` (16 byte) check.

# v1.7.0 (2026-03-09)

### Security fixes

- **TLS certificate verification is now enabled by default.** Prior to this release `tls_insecure` defaulted to `true`, meaning TLS connections to Redis did not verify the server certificate. The default is now `false`. Users who require TLS without a verifiable certificate must explicitly set `tls_insecure true` in their configuration.
- **`key_prefix` is now validated and normalised.** Leading and trailing `/` characters are stripped, and prefixes containing empty segments or path traversal segments (`.` or `..`) are rejected at startup.
- **Encryption key is no longer included in startup error messages.** Previously, a too-short `encryption_key` value was echoed in the error message.
- **Decompression bomb protection.** Decompression is now limited to 4 MiB. Values that decompress beyond this limit are rejected with an error.

### Improvements

- Updated `go-redis` from v9.17.2 to v9.18.0.
- Added `miniredis/v2` as a test dependency to enable unit tests without a live Redis instance.
- Added a GitHub Actions workflow that runs unit tests on pull requests.
- Expanded test coverage for configuration validation, encryption, and compression edge cases.

# v1.6.0 (2026-02-24)

This version adds support for Caddy Server 2.11.1 which introduced a breaking change
to the return parameters of function caddycmd.LoadConfig() which is used by this module.

Several important module dependencies were updated for security reasons in this release:
 - github.com/redis/go-redis from v9.3.0 to v9.18
 - github.com/spf13/cobra from v1.7.0 to v1.10.2
 - github.com/stretchr/testify from v1.9.0 to v1.11.1
 - go.uber.org/zap from v1.25.0 to v1.27.1

As well as many indirect dependency upgrades resulting from the above changes.

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

Updated documentation and project dependencies only. No functional changes included.

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
