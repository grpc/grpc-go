#!/bin/bash -e
cd "$(dirname "$0")"
BIN="./interop-testing/build/install/grpc-interop-testing/bin/test-server"
if [[ ! -e "$BIN" ]]; then
  cat >&2 <<EOF
Could not find binary. It can be built with:
./gradlew :grpc-interop-testing:installDist -PskipCodegen=true
EOF
  exit 1
fi
exec "$BIN" "$@"
