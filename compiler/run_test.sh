#!/bin/bash

if [ -z "$TEST_TMP_DIR" ]; then
  echo '$TEST_TMP_DIR not set'
  exit 1;
fi

cd $(dirname $0)

TEST_SRC_DIR='test'
INPUT_FILE="$TEST_SRC_DIR/test.proto"
OUTPUT_FILE="$TEST_TMP_DIR/TestServiceGrpc.src.jar"
GOLDEN_FILE="$TEST_SRC_DIR/TestService.java.txt"

protoc --plugin=protoc-gen-java_rpc=build/binaries/java_pluginExecutable/java_plugin \
  --java_rpc_out="$OUTPUT_FILE" --proto_path="$TEST_SRC_DIR" "$INPUT_FILE" && \
  unzip -o -d "$TEST_TMP_DIR" "$OUTPUT_FILE" && \
  diff "$TEST_TMP_DIR/com/google/net/stubby/testing/integration/TestServiceGrpc.java" \
    "$GOLDEN_FILE" && \
  echo "PASS"
