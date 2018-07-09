#!/bin/bash

set -exu -o pipefail
cat /VERSION

BASE_DIR="$(pwd)"

# Install gRPC and codegen for the Android examples
# (a composite gradle build can't find protoc-gen-grpc-java)

cd "$BASE_DIR/github/grpc-java"

export GRADLE_OPTS=-Xmx512m
export LDFLAGS=-L/tmp/protobuf/lib
export CXXFLAGS=-I/tmp/protobuf/include
export LD_LIBRARY_PATH=/tmp/protobuf/lib
export OS_NAME=$(uname)

# Proto deps
buildscripts/make_dependencies.sh

./gradlew install

# Build grpc-cronet

pushd cronet
../gradlew build
popd

# Build grpc-android

pushd android
../gradlew build
popd

# Build examples

cd ./examples/android/clientcache
./gradlew build
cd ../routeguide
./gradlew build
cd ../helloworld
./gradlew build

cd "$BASE_DIR/github/grpc-java/examples/example-kotlin/android/helloworld/"
./gradlew build

# Skip APK size and dex count comparisons for non-PR builds

if [[ -z "${KOKORO_GITHUB_PULL_REQUEST_COMMIT:-}" ]]; then
    echo "Skipping APK size and dex count"
    exit 0
fi

# Save a copy of set_github_status.py (it may differ from the base commit)

SET_GITHUB_STATUS="$TMPDIR/set_github_status.py"
cp "$BASE_DIR/github/grpc-java/buildscripts/set_github_status.py" "$SET_GITHUB_STATUS"


# Collect APK size and dex count stats for the helloworld example

HELLO_WORLD_OUTPUT_DIR="$BASE_DIR/github/grpc-java/examples/android/helloworld/app/build/outputs"

read -r ignored new_dex_count < \
  <("${ANDROID_HOME}/tools/bin/apkanalyzer" dex references \
  "$HELLO_WORLD_OUTPUT_DIR/apk/release/app-release-unsigned.apk")

set +x
all_new_methods=`"${ANDROID_HOME}/tools/bin/apkanalyzer" dex packages \
  --proguard-mapping "$HELLO_WORLD_OUTPUT_DIR/mapping/release/mapping.txt" \
  "$HELLO_WORLD_OUTPUT_DIR/apk/release/app-release-unsigned.apk" | grep ^M | cut -f4 | sort`
set -x

new_apk_size="$(stat --printf=%s $HELLO_WORLD_OUTPUT_DIR/apk/release/app-release-unsigned.apk)"


# Get the APK size and dex count stats using the pull request base commit

cd $BASE_DIR/github/grpc-java
git checkout HEAD^
./gradlew install
cd examples/android/helloworld/
./gradlew build

read -r ignored old_dex_count < \
  <("${ANDROID_HOME}/tools/bin/apkanalyzer" dex references app/build/outputs/apk/release/app-release-unsigned.apk)

set +x
all_old_methods=`"${ANDROID_HOME}/tools/bin/apkanalyzer" dex packages --proguard-mapping app/build/outputs/mapping/release/mapping.txt app/build/outputs/apk/release/app-release-unsigned.apk | grep ^M | cut -f4 | sort`
set -x

old_apk_size="$(stat --printf=%s app/build/outputs/apk/release/app-release-unsigned.apk)"

dex_count_delta="$((new_dex_count-old_dex_count))"

apk_size_delta="$((new_apk_size-old_apk_size))"

set +x
dex_method_diff=`diff -u <(echo "$all_old_methods") <(echo "$all_new_methods") || true`
set -x

if [[ -n "$dex_method_diff" ]]
then
  echo "Method diff: ${dex_method_diff}"
fi

# Update the statuses with the deltas

gsutil cp gs://grpc-testing-secrets/github_credentials/oauth_token.txt ~/

"$SET_GITHUB_STATUS" \
  --sha1 "$KOKORO_GITHUB_PULL_REQUEST_COMMIT" \
  --state success \
  --description "New DEX reference count: $(printf "%'d" "$new_dex_count") (delta: $(printf "%'d" "$dex_count_delta"))" \
  --context android/dex_diff --oauth_file ~/oauth_token.txt

"$SET_GITHUB_STATUS" \
  --sha1 "$KOKORO_GITHUB_PULL_REQUEST_COMMIT" \
  --state success \
  --description "New APK size in bytes: $(printf "%'d" "$new_apk_size") (delta: $(printf "%'d" "$apk_size_delta"))" \
  --context android/apk_diff --oauth_file ~/oauth_token.txt
