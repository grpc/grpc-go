#!/bin/bash -e
cd "$(dirname "$0")"
cat >&2 <<EOF
Gradle is no longer run automatically. Make sure to run './gradlew installDist' or
'./gradlew :grpc-integration-testing:installDist' after any changes.
EOF
exec ./integration-testing/build/install/grpc-integration-testing/bin/test-client "$@"
