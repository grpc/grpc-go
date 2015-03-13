#!/bin/bash

if [ -z "$TEST_TMP_DIR" ]; then
  echo '$TEST_TMP_DIR not set'
  exit 1;
fi

cd "$(dirname "$0")"

INPUT_FILE="proto/test.proto"
OUTPUT_FILE="$TEST_TMP_DIR/TestServiceGrpc.src.jar"
GRPC_FILE="$TEST_TMP_DIR/io/grpc/testing/integration/TestServiceGrpc.java"
GOLDEN_FILE="golden/TestServiceNano.java.txt"

protoc --plugin=protoc-gen-java_rpc=../../build/binaries/java_pluginExecutable/java_plugin \
  --java_rpc_out=nano=true:"$OUTPUT_FILE" "$INPUT_FILE" && \
  unzip -o -d "$TEST_TMP_DIR" "$OUTPUT_FILE" && \
  diff "$GRPC_FILE" "$GOLDEN_FILE" && \
  echo "PASS"
