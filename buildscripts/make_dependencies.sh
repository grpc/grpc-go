#!/bin/bash
#
# Build protoc
set -ev

DOWNLOAD_DIR=/tmp/source
INSTALL_DIR="/tmp/protobuf-$PROTOBUF_VERSION/$(uname -s)-$(uname -p)"
mkdir -p $DOWNLOAD_DIR

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
  make -j$(nproc)
  make install
  popd
fi

