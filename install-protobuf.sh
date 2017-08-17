#!/usr/bin/env bash

set -ex

die() {
    echo "$@" >&2
    exit 1
}

case "$PROTOBUF_VERSION" in
3*)
    basename=protoc-$PROTOBUF_VERSION
    ;;
*)
    die "unknown protobuf version: $PROTOBUF_VERSION"
    ;;
esac

cd /home/travis

wget https://github.com/google/protobuf/releases/download/v$PROTOBUF_VERSION/$basename-linux-x86_64.zip
unzip $basename-linux-x86_64.zip
bin/protoc --version
