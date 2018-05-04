#!/bin/bash
set -veux -o pipefail

if [[ -f /VERSION ]]; then
  cat /VERSION
fi

readonly GRPC_JAVA_DIR="$(cd "$(dirname "$0")"/../.. && pwd)"

rm -rf /tmp/protobuf/
mkdir -p /tmp/protobuf/
# Download an unreleased SHA because we need this fix:
# https://github.com/google/protobuf/pull/4447
wget -O - https://github.com/google/protobuf/archive/92898e9e.tar.gz | tar xz -C /tmp/protobuf/

docker build -t protoc-artifacts /tmp/protobuf/protobuf-92898e9e9cb2f1c006fcc5099c9c96eafce63dc8/protoc-artifacts

docker build -t grpc-java-releasing "$GRPC_JAVA_DIR"/buildscripts/grpc-java-releasing

"$GRPC_JAVA_DIR"/buildscripts/run_in_docker.sh /grpc-java/buildscripts/build_artifacts_in_docker.sh

# grpc-android requires the Android SDK, so build outside of Docker and
# use --include-build for its grpc-core dependency
LOCAL_MVN_TEMP=$(mktemp -d)
pushd "$GRPC_JAVA_DIR/android"
../gradlew uploadArchives \
  --include-build "$GRPC_JAVA_DIR" \
  -Dorg.gradle.parallel=false \
  -PskipCodegen=true \
  -PrepositoryDir="$LOCAL_MVN_TEMP"
popd

readonly MVN_ARTIFACT_DIR="${MVN_ARTIFACT_DIR:-$GRPC_JAVA_DIR/mvn-artifacts}"
mkdir -p "$MVN_ARTIFACT_DIR"
cp -r "$LOCAL_MVN_TEMP"/* "$MVN_ARTIFACT_DIR"/
