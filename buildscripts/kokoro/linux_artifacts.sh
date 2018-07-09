#!/bin/bash
set -veux -o pipefail

if [[ -f /VERSION ]]; then
  cat /VERSION
fi

readonly GRPC_JAVA_DIR="$(cd "$(dirname "$0")"/../.. && pwd)"

"$GRPC_JAVA_DIR"/buildscripts/build_docker.sh
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
