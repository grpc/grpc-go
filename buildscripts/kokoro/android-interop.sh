#!/bin/bash

set -exu -o pipefail
if [[ -f /VERSION ]]; then
  cat /VERSION
fi

# Install gRPC and codegen for the Android interop app
# (a composite gradle build can't find protoc-gen-grpc-java)

cd github/grpc-java

export GRADLE_OPTS=-Xmx512m
export LDFLAGS=-L/tmp/protobuf/lib
export CXXFLAGS=-I/tmp/protobuf/include
export LD_LIBRARY_PATH=/tmp/protobuf/lib
export OS_NAME=$(uname)

echo y | ${ANDROID_HOME}/tools/bin/sdkmanager "build-tools;28.0.3"

# Proto deps
buildscripts/make_dependencies.sh

./gradlew publishToMavenLocal


# Build and run interop instrumentation tests on Firebase Test Lab
cd android-interop-testing
../gradlew assembleDebug
../gradlew assembleDebugAndroidTest
gcloud firebase test android run \
  --type instrumentation \
  --app app/build/outputs/apk/debug/app-debug.apk \
  --test app/build/outputs/apk/androidTest/debug/app-debug-androidTest.apk \
  --environment-variables \
      server_host=grpc-test.sandbox.googleapis.com,server_port=443,test_case=all \
  --device model=Nexus6P,version=27,locale=en,orientation=portrait \
  --device model=Nexus6P,version=26,locale=en,orientation=portrait \
  --device model=Nexus6P,version=25,locale=en,orientation=portrait \
  --device model=Nexus6P,version=24,locale=en,orientation=portrait \
  --device model=Nexus6P,version=23,locale=en,orientation=portrait \
  --device model=Nexus6,version=22,locale=en,orientation=portrait \
  --device model=Nexus6,version=21,locale=en,orientation=portrait
