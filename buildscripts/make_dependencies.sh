#!/bin/bash
#
# Build protoc
set -evux -o pipefail

PROTOBUF_VERSION=3.5.1

# ARCH is 64 bit unless otherwise specified.
ARCH="${ARCH:-64}"
DOWNLOAD_DIR=/tmp/source
INSTALL_DIR="/tmp/protobuf-cache/$PROTOBUF_VERSION/$(uname -s)-$(uname -p)-x86_$ARCH"
mkdir -p $DOWNLOAD_DIR

# Start with a sane default
NUM_CPU=4
if [[ $(uname) == 'Linux' ]]; then
    NUM_CPU=$(nproc)
fi
if [[ $(uname) == 'Darwin' ]]; then
    NUM_CPU=$(sysctl -n hw.ncpu)
fi

# Make protoc
# Can't check for presence of directory as cache auto-creates it.
if [ -f ${INSTALL_DIR}/bin/protoc ]; then
  echo "Not building protobuf. Already built"
# TODO(ejona): swap to `brew install --devel protobuf` once it is up-to-date
else
  if [[ ! -d "$DOWNLOAD_DIR"/protobuf-"${PROTOBUF_VERSION}" ]]; then
    wget -O - https://github.com/google/protobuf/releases/download/v${PROTOBUF_VERSION}/protobuf-all-${PROTOBUF_VERSION}.tar.gz | tar xz -C $DOWNLOAD_DIR
  fi
  pushd $DOWNLOAD_DIR/protobuf-${PROTOBUF_VERSION}
  # install here so we don't need sudo
  ./configure CFLAGS=-m"$ARCH" CXXFLAGS=-m"$ARCH" --disable-shared \
    --prefix="$INSTALL_DIR"
  # the same source dir is used for 32 and 64 bit builds, so we need to clean stale data first
  make clean
  make -j$NUM_CPU
  make install
  popd
fi

# If /tmp/protobuf exists then we just assume it's a symlink created by us.
# It may be that it points to the wrong arch, so we idempotently set it now.
if [[ -L /tmp/protobuf ]]; then
  rm /tmp/protobuf
fi
ln -s "$INSTALL_DIR" /tmp/protobuf

cat <<EOF
To compile with the build dependencies:

export LDFLAGS=-L/tmp/protobuf/lib
export CXXFLAGS=-I/tmp/protobuf/include
export LD_LIBRARY_PATH=/tmp/protobuf/lib
EOF
