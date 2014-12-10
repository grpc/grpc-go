#!/bin/bash -e
TARGET='Test Service Client'
TARGET_CLASS='com.google.net.stubby.testing.integration.TestServiceClient'
TARGET_ARGS="$@"

cd "$(dirname "$0")"
mvn -q -nsu -pl integration-testing -am package -Dcheckstyle.skip=true -DskipTests
. integration-testing/target/bootclasspath.properties
echo "[INFO] Running: $TARGET ($TARGET_CLASS $TARGET_ARGS)"
exec java "$bootclasspath" -cp "$jar" "$TARGET_CLASS" $TARGET_ARGS
