# gRPC-Go Environment Variables

This document lists the environment variables supported by the grpc-go
implementation.

Variables whose names contain `EXPERIMENTAL` are intentionally not documented
here: they guard features that are still in development, and they may change
behavior, change defaults, or be removed entirely in any release without
notice.

Unless stated otherwise, boolean variables are case-insensitive and only the
values `true` and `false` are recognized; any other value leaves the default
in effect.

## Logging

See [Log Levels](Documentation/log_levels.md) for a description of the log
severities and how they are used.

*   `GRPC_GO_LOG_SEVERITY_LEVEL`

    The minimum severity of log messages written to stderr by the default
    logger. One of `ERROR`, `WARNING` or `INFO` (e.g. `INFO` enables info,
    warning and error logs). Defaults to `ERROR`. Has no effect if the
    application replaces the default logger via
    [`grpclog.SetLoggerV2`](https://pkg.go.dev/google.golang.org/grpc/grpclog#SetLoggerV2).

*   `GRPC_GO_LOG_VERBOSITY_LEVEL`

    The verbosity of info log messages, as a non-negative integer. Info logs
    at verbosity levels less than or equal to this value are emitted (subject
    to `GRPC_GO_LOG_SEVERITY_LEVEL` enabling info logs). Defaults to `0`.

*   `GRPC_GO_LOG_FORMATTER`

    Set to `json` to make the default logger emit log messages as JSON
    objects. Any other value uses the default plain-text format.

## Binary logging

*   `GRPC_BINARY_LOG_FILTER`

    Enables binary logging and selects which methods are logged, as described
    in [gRFC A16](https://github.com/grpc/proposal/blob/master/A16-binary-logging.md).
    The value is a comma-separated list of method patterns, e.g. `*` (all
    methods), `package.Service/Method`, `package.Service/*`, or
    `-package.Service/Method` to exclude a method. A pattern may be suffixed
    with `{h[:len]}`, `{m[:len]}` or `{h[:len];m[:len]}` to limit the logged
    header and message sizes. Unset or empty disables binary logging.

## Name resolution

*   `GRPC_ENABLE_TXT_SERVICE_CONFIG`

    Whether the DNS resolver performs TXT record lookups to retrieve the
    service config, as described in
    [gRFC A2](https://github.com/grpc/proposal/blob/master/A2-service-configs-in-dns.md).
    Defaults to `true`.

*   `GRPC_GO_IGNORE_TXT_ERRORS`

    Whether the DNS resolver ignores errors from TXT record lookups. When
    `true`, TXT lookup failures are logged but resolution proceeds without a
    service config; when `false`, the error is reported to the channel.
    Defaults to `true`.

## xDS

*   `GRPC_XDS_BOOTSTRAP`

    Path to a file containing the xDS bootstrap configuration in JSON format.
    Takes precedence over `GRPC_XDS_BOOTSTRAP_CONFIG` if both are set.

*   `GRPC_XDS_BOOTSTRAP_CONFIG`

    The xDS bootstrap configuration itself, in JSON format. Used only if
    `GRPC_XDS_BOOTSTRAP` is unset.

## Load balancing

*   `GRPC_RING_HASH_CAP`

    The maximum ring size for the
    [`ring_hash`](https://github.com/grpc/proposal/blob/master/A42-xds-ring-hash-lb-policy.md)
    load balancing policy. Configured ring sizes are capped at this value.
    Defaults to `4096`; values are clamped to the range `[1, 8388608]` (8M).
    This does not affect config validation, which rejects ring sizes larger
    than 8M.

*   `GRPC_XDS_ENDPOINT_HASH_KEY_BACKWARD_COMPAT`

    Set to `true` to restore the behavior that predates the endpoint hash key
    support from
    [gRFC A76](https://github.com/grpc/proposal/blob/master/A76-ring-hash-improvements.md),
    ignoring the hash key from EDS endpoint metadata. Defaults to `false`.
    This variable is transitional and will be removed in a future release.

## Security

*   `GRPC_ENFORCE_ALPN_ENABLED`

    Whether TLS connections to peers that do not negotiate ALPN are rejected.
    HTTP/2 requires ALPN; this variable exists only for backward compatibility
    with non-compliant peers, and may be removed in a future release. Defaults
    to `true`.

*   `GRPC_ALTS_MAX_CONCURRENT_HANDSHAKES`

    The maximum number of concurrent ALTS handshakes. Defaults to `100`;
    values are clamped to the range `[1, 100]`.

## Server

*   `GRPC_GO_SERVER_GOROUTINE_LABELS`

    Controls the [runtime/pprof labels](https://pkg.go.dev/runtime/pprof#Labels)
    set on goroutines spawned by `grpc.Server` to handle incoming requests.
    The value is a comma-separated list of `label=true|false` entries;
    `grpc.method` is currently the only supported label. The values `all` and
    `none` enable and disable all supported labels. Defaults to no labels.

## GCP observability

Used by the [gcp/observability](https://pkg.go.dev/google.golang.org/grpc/gcp/observability)
package; see its documentation for the configuration schema.

*   `GRPC_GCP_OBSERVABILITY_CONFIG_FILE`

    Path to a file containing the observability configuration in JSON format.
    Takes precedence over `GRPC_GCP_OBSERVABILITY_CONFIG` if both are set.

*   `GRPC_GCP_OBSERVABILITY_CONFIG`

    The observability configuration itself, in JSON format. Used only if
    `GRPC_GCP_OBSERVABILITY_CONFIG_FILE` is unset.
