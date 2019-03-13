#!/bin/bash
set -eu -o pipefail

readonly proto_dir="$(mktemp -d --tmpdir protobuf.XXXXXX)"
wget -O - https://github.com/google/protobuf/archive/v3.7.0.tar.gz | tar xz -C "$proto_dir"

docker build -t protoc-artifacts "$proto_dir"/protobuf-3.7.0/protoc-artifacts
rm -r "$proto_dir"
