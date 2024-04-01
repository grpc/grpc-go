#!/bin/bash

set -ex # Debugging enabled.

die() {
    echo "$@" >&2
    exit 1
}

trap "git clean --force --quiet" EXIT

WD="$(dirname $0)"

protoc \
    --go_out=. \
    --go_opt=paths=source_relative \
    --go-grpc_out=. \
    --go-grpc_opt=paths=source_relative \
    "$WD/proto/golden.proto"

if !(diff -u "$WD/testdata/golden_grpc.pb.go" "$WD/proto/golden_grpc.pb.go"); then
    die "Generated file golden_grpc.pb.go differs from golden file; If you have made recent changes to protoc-gen-go-grpc, please regenerate the golden files."
fi

echo SUCCESS
