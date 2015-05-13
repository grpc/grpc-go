#!/bin/bash
#
# Build protoc & netty
set -ev

# Make protoc
# Can't check for presence of directory as cache auto-creates it.
if [ -f /tmp/proto3-a3/bin/protoc ]; then
  echo "Not building protobuf. Already built"
else
  wget -O - https://github.com/google/protobuf/archive/v3.0.0-alpha-3.1.tar.gz | tar xz -C /tmp
  pushd /tmp/protobuf-3.0.0-alpha-3.1
  ./autogen.sh
  # install here so we don't need sudo
  ./configure --prefix=/tmp/proto3-a3
  make -j2
  make install
  popd
fi
