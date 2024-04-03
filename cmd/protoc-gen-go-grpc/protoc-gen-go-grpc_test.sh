#!/bin/bash

set -ex # Debugging enabled.

trap "git clean --force --quiet" EXIT

# Build protoc-gen-go-grpc binary and add to $PATH.
go build google.golang.org/grpc/cmd/protoc-gen-go-grpc
PATH="${PWD}:${PATH}"

WD="$(dirname $0)"
protoc \
    --go-grpc_out=. \
    --go-grpc_opt=paths=source_relative \
    "$WD/proto/golden.proto"

if !(diff -u "${WD}/testdata/golden_grpc.pb.go" "${WD}/proto/golden_grpc.pb.go"); then
    echo "Generated file golden_grpc.pb.go differs from golden file; If you have made recent changes to protoc-gen-go-grpc, please regenerate the golden files." >&2
    exit 1
fi

echo SUCCESS
