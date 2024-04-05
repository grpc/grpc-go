#!/bin/bash

source "$(dirname $0)/util.sh"

if [[ "${GITHUB_ACTIONS}" = "true" ]]; then
    PROTOBUF_VERSION=25.2 # Shows up in pb.go files as v4.22.0
    PROTOC_FILENAME=protoc-${PROTOBUF_VERSION}-linux-x86_64.zip
    pushd /home/runner/go
    wget https://github.com/google/protobuf/releases/download/v${PROTOBUF_VERSION}/${PROTOC_FILENAME}
    unzip ${PROTOC_FILENAME}
    bin/protoc --version
    popd
elif not which protoc > /dev/null; then
    echo "Please install protoc into your path">&2
    exit 1
fi

# - Check that generated proto files are up to date.
go generate google.golang.org/grpc/... && git status --porcelain 2>&1 | fail_on_output || \
(git status; git --no-pager diff; exit 1)

echo SUCCESS
exit 0