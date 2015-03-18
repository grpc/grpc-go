#!/bin/bash
#
# Build protoc & netty
set -ev

# If we need GCC 4.8 for C++11 support which is not available by default with travis
# and we don't want to use sudo on travis to install it as we couldn't use dockerized travis so...
#pushd .
#cd /tmp
#mkdir gcc
#cd gcc
#apt-get download -qq gcc-4.8 g++-4.8 cpp-4.8 libgcc-4.8-dev libstdc++-4.8-dev g++-4.8-multilib gcc-4.8-multilib
#find . -name '*.deb' -exec dpkg --extract {} . \;
#export CXX="/tmp/gcc/usr/bin/g++-4.8"
#export CC="/tmp/gcc/usr/bin/gcc-4.8"
#popd

export CXXFLAGS="--std=c++0x"
# Make protoc
pushd .
cd /tmp
wget -O - https://github.com/google/protobuf/archive/v3.0.0-alpha-2.tar.gz | tar xz
cd protobuf-3.0.0-alpha-2
./autogen.sh
# install here so we don't need sudo
./configure --prefix=/tmp/grpc-deps
make -j2
# make check -j2
make install
cd java
mvn install
cd ../javanano
mvn install
popd

# Make and install netty
git submodule update --init
pushd .
cd lib/netty
mvn install -pl codec-http2 -am -DskipTests=true
popd