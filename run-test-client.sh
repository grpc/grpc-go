#!/bin/bash -e
cd "$(dirname "$0")"
./gradlew :grpc-integration-testing:installDist
./integration-testing/build/install/grpc-integration-testing/bin/test-client "$@"
