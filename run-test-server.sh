#!/bin/bash -e
TARGET='Test Service Server'
TARGET_CLASS='io.grpc.testing.integration.TestServiceServer'

TARGET_ARGS=''
for i in "$@"; do 
    TARGET_ARGS="$TARGET_ARGS, '$i'"
done
TARGET_ARGS="${TARGET_ARGS:2}"

cd "$(dirname "$(readlink -f "$0")")"
echo "[INFO] Running: $TARGET ($TARGET_CLASS $TARGET_ARGS)"
./gradlew -PmainClass="$TARGET_CLASS" -PappArgs="[$TARGET_ARGS]" :grpc-integration-testing:execute
