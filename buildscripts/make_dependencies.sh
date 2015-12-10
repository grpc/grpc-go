#!/bin/bash
#
# Build protoc & openssl
set -ev

DOWNLOAD_DIR=/tmp/source
INSTALL_DIR=/tmp/protobuf-${PROTOBUF_VERSION}
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
  ./configure --prefix=${INSTALL_DIR}
  make -j$(nproc)
  make install
  popd
fi

INSTALL_DIR=/tmp/openssl-${OPENSSL_VERSION}

if [ -f ${INSTALL_DIR}/lib/libssl.so ]; then
  echo "Not building openssl. Already built"
elif [ "$(uname)" = Darwin ]; then
  brew install openssl
else
  # The version without the patch letter (e.g., 1.0.2 provided 1.0.2d)
  VERSION_BASE=${OPENSSL_VERSION%%[a-z]*}
  wget -O - https://www.openssl.org/source/old/$VERSION_BASE/openssl-${OPENSSL_VERSION}.tar.gz \
    | tar xz -C $DOWNLOAD_DIR
  pushd $DOWNLOAD_DIR/openssl-${OPENSSL_VERSION}
  ./Configure linux-x86_64 shared no-ssl2 no-comp --prefix=${INSTALL_DIR}
  make -j$(nproc)
  make install
  popd
fi
