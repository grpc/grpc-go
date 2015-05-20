#!/bin/bash -e
cd "$(dirname "$0")"
cat >&2 <<EOF
Gradle is no longer run automatically. Make sure to run './gradlew installDist' or
'./gradlew :grpc-interop-testing:installDist' after any changes.
EOF
exec ./interop-testing/build/install/grpc-interop-testing/bin/test-server "$@"
