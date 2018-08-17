#!/bin/bash
set -eu -o pipefail

readonly proto_dir="$(mktemp -d protobuf.XXXXXX)"
# Download an unreleased SHA to include TLS 1.2 support:
# https://github.com/google/protobuf/pull/4879
wget -O - https://github.com/google/protobuf/archive/61476b8e74357ea875f71bb321874ca4530b7d50.tar.gz | tar xz -C "$proto_dir"

docker build -t protoc-artifacts "$proto_dir"/protobuf-61476b8e74357ea875f71bb321874ca4530b7d50/protoc-artifacts
rm -r "$proto_dir"
