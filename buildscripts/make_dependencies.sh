#!/bin/bash
#
# Build protoc & netty
set -ev

# Make protoc
# Can't check for presence of directory as cache auto-creates it.
if [ -f /tmp/proto3-a3/bin/protoc ]; then
  echo "Not building protobuf. Already built"
else
  wget -O - https://github.com/google/protobuf/archive/v3.0.0-alpha-3.tar.gz | tar xz -C /tmp
  pushd /tmp/protobuf-3.0.0-alpha-3
  ./autogen.sh
  # install here so we don't need sudo
  ./configure --prefix=/tmp/proto3-a3
  make -j2
  make install
  popd
fi

# Make and install netty
pushd lib/netty
BUILD_NETTY=1
NETTY_REV_FILE="$HOME/.m2/repository/io/netty/netty-ver"
REV="$(git rev-parse HEAD)"
if [ -f "$NETTY_REV_FILE" ]; then
  REV_LAST="$(cat "$NETTY_REV_FILE")"
  if [ z"$REV" = z"$REV_LAST" ]; then
    BUILD_NETTY=0
    echo "Not building Netty; already at $REV"
  fi
fi
if [ $BUILD_NETTY = 1 ]; then
  mvn install -pl codec-http2 -am -DskipTests=true
  echo "$REV" > "$NETTY_REV_FILE"
fi
popd
