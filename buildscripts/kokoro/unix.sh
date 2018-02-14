#!/bin/bash

# This file is used for both Linux and MacOS builds.
# To run locally:
#  ./buildscripts/kokoro/unix.sh
# Optionally set MVN_ARTIFACTS=true to retain artifacts

# This script assumes `set -e`. Removing it may lead to undefined behavior.
set -exu -o pipefail

if [[ -f /VERSION ]]; then
  cat /VERSION
fi

# cd to the root dir of grpc-java
cd $(dirname $0)/../..

# TODO(zpencer): always make sure we are using Oracle jdk8

# Proto deps
export PROTOBUF_VERSION=3.5.1

# TODO(zpencer): if linux builds use this script, then also repeat this process for 32bit (-m32)
# Today, only macos uses this script and macos targets 64bit only

CXX_FLAGS="-m64" LDFLAGS="" LD_LIBRARY_PATH="" buildscripts/make_dependencies.sh

# the install dir is hardcoded in make_dependencies.sh
PROTO_INSTALL_DIR="/tmp/protobuf-${PROTOBUF_VERSION}/$(uname -s)-$(uname -p)"

if [[ ! -e /tmp/protobuf ]]; then
  ln -s $PROTO_INSTALL_DIR /tmp/protobuf;
fi

# It's better to use 'readlink -f' but it's not available on macos
if [[ "$(readlink /tmp/protobuf)" != "$PROTO_INSTALL_DIR" ]]; then
  echo "/tmp/protobuf already exists but is not a symlink to $PROTO_INSTALL_DIR"
  exit 1;
fi

# Set properties via flags, do not pollute gradle.properties
GRADLE_FLAGS="${GRADLE_FLAGS:-}"
GRADLE_FLAGS+=" -Pcheckstyle.ignoreFailures=false"
GRADLE_FLAGS+=" -PfailOnWarnings=true"
GRADLE_FLAGS+=" -PerrorProne=true"
export GRADLE_OPTS="-Xmx512m"

# Make protobuf discoverable by :grpc-compiler
export LD_LIBRARY_PATH=/tmp/protobuf/lib
export LDFLAGS=-L/tmp/protobuf/lib
export CXXFLAGS="-I/tmp/protobuf/include"

# Run tests
./gradlew assemble generateTestProto install $GRADLE_FLAGS
pushd examples
./gradlew build $GRADLE_FLAGS
# --batch-mode reduces log spam
mvn verify --batch-mode
popd
# TODO(zpencer): also build the GAE examples

LOCAL_MVN_TEMP=$(mktemp -d)
./gradlew clean grpc-compiler:build grpc-compiler:uploadArchives -PtargetArch=x86_64 \
  -Dorg.gradle.parallel=false -PrepositoryDir=$LOCAL_MVN_TEMP $GRADLE_FLAGS

if [[ -z "${MVN_ARTIFACTS:-}" ]]; then
  exit 0
fi
MVN_ARTIFACT_DIR="$PWD/mvn-artifacts"
mkdir $MVN_ARTIFACT_DIR
mv $LOCAL_MVN_TEMP/* $MVN_ARTIFACT_DIR
rmdir $LOCAL_MVN_TEMP
