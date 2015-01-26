#!/bin/bash -e
TARGET='Test Service Client'
TARGET_CLASS='com.google.net.stubby.testing.integration.TestServiceClient'

TARGET_ARGS=''
for i in "$@"; do 
    TARGET_ARGS="$TARGET_ARGS, '$i'"
done
TARGET_ARGS="${TARGET_ARGS:2}"

echo "[INFO] Running: $TARGET ($TARGET_CLASS $TARGET_ARGS)"
gradle -PmainClass="$TARGET_CLASS" -PappArgs="[$TARGET_ARGS]" :stubby-integration-testing:execute
