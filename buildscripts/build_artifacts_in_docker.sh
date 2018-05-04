#!/bin/bash
set -exu -o pipefail

# Runs all the tests and builds mvn artifacts.
# mvn artifacts are stored in grpc-java/mvn-artifacts/
ALL_ARTIFACTS=true ARCH=64 "$(dirname $0)"/kokoro/unix.sh
# Already ran tests the first time, so skip tests this time
SKIP_TESTS=true ARCH=32 "$(dirname $0)"/kokoro/unix.sh
