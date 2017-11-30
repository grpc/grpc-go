#!/bin/bash

# This file is used for both Linux and MacOS builds.
# TODO(zpencer): test this script for Linux

# This script assumes `set -e`. Removing it may lead to undefined behavior.
set -exu -o pipefail

export GRADLE_OPTS=-Xmx512m
export PROTOBUF_VERSION=3.5.0
export LDFLAGS=-L/tmp/protobuf/lib
export CXXFLAGS=-I/tmp/protobuf/include
export LD_LIBRARY_PATH=/tmp/protobuf/lib
export OS_NAME=$(uname)

cd ./github/grpc-java

# TODO(zpencer): always make sure we are using Oracle jdk8

# Proto deps
buildscripts/make_dependencies.sh
ln -s "/tmp/protobuf-${PROTOBUF_VERSION}/$(uname -s)-$(uname -p)" /tmp/protobuf

# Gradle build config
mkdir -p $HOME/.gradle
echo "checkstyle.ignoreFailures=false" >> $HOME/.gradle/gradle.properties
echo "failOnWarnings=true" >> $HOME/.gradle/gradle.properties
echo "errorProne=true" >> $HOME/.gradle/gradle.properties

# Run tests
./gradlew assemble generateTestProto install
pushd examples
./gradlew build
# --batch-mode reduces log spam
mvn verify --batch-mode
popd
# TODO(zpencer): also build the GAE examples
