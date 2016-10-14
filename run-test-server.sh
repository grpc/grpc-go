#!/bin/bash -e
cd "$(dirname "$0")"
cat >&2 <<EOF
Gradle is no longer run automatically. Make sure to run
'./gradlew installDist -PskipCodegen=true' or
'./gradlew :grpc-interop-testing:installDist -PskipCodegen=true' after any
changes. -PskipCodegen=true is optional, but requires less setup.
EOF
exec ./interop-testing/build/install/grpc-interop-testing/bin/test-server "$@"
