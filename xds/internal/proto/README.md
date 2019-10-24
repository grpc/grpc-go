# Generated files

Generated files in `./envoy` are copied from
github.com/envoyproxy/go-control-plane.

To regenerate, update `GO_CONTROL_PLANE_VERSION` in `copy_xds_pbgo.sh`, and run
it in this directory.

## Why copy

Another approach would be to import `go-control-plane` directly. But that would
add `go-control-plane` and it's dependencies to module grpc (`go.mod`). This can
be solved by moving `xds` to a sub module.