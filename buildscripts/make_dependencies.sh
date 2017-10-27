#!/bin/bash
#
# Build protoc
set -evux -o pipefail

DOWNLOAD_DIR=/tmp/source
INSTALL_DIR="/tmp/protobuf-$PROTOBUF_VERSION/$(uname -s)-$(uname -p)"
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
  wget -O - https://github.com/google/protobuf/archive/v${PROTOBUF_VERSION}.tar.gz | tar xz -C $DOWNLOAD_DIR
  pushd $DOWNLOAD_DIR/protobuf-${PROTOBUF_VERSION}
  ./autogen.sh
  # install here so we don't need sudo
  ./configure --prefix="$INSTALL_DIR"
  make -j$NUM_CPU
  make install
  popd
fi

