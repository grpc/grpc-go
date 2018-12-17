#!/bin/bash
# Copyright 2018 The gRPC Authors
#
# Licensed under the Apache License, Version 2.0 (the "License");
# you may not use this file except in compliance with the License.
# You may obtain a copy of the License at
#
#     http://www.apache.org/licenses/LICENSE-2.0
#
# Unless required by applicable law or agreed to in writing, software
# distributed under the License is distributed on an "AS IS" BASIS,
# WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
# See the License for the specific language governing permissions and
# limitations under the License.

# in this directory run the following commands
BRANCH=master
# import GIT_ORIGIN_REV_ID from one of the google internal CLs
GIT_ORIGIN_REV_ID=8e6aaf55f4954f1ef9d3ee2e8f5a50e79cc04f8f
GIT_REPO="https://github.com/lyft/protoc-gen-validate.git"
GIT_BASE_DIR=protoc-gen-validate
SOURCE_PROTO_BASE_DIR=protoc-gen-validate
TARGET_PROTO_BASE_DIR=src/main/proto
FILES=(
validate/validate.proto
)

# clone the protoc-gen-validate github repo in /tmp directory
pushd /tmp
rm -rf $GIT_BASE_DIR
git clone -b $BRANCH $GIT_REPO
cd $GIT_BASE_DIR
git checkout $GIT_ORIGIN_REV_ID
popd

cp -p /tmp/${GIT_BASE_DIR}/LICENSE LICENSE

mkdir -p ${TARGET_PROTO_BASE_DIR}
pushd ${TARGET_PROTO_BASE_DIR}

# copy proto files to project directory
for file in "${FILES[@]}"
do
  mkdir -p $(dirname ${file})
  cp -p /tmp/${SOURCE_PROTO_BASE_DIR}/${file} ${file}
done

for file in "${FILES[@]}"
do
  # remove old "option java_multiple_files" if any
  sed -i -e '/^option\sjava_multiple_files\s=/d' $file
  # add new "option java_multiple_files"
  sed -i -e "/^package\s/a option java_multiple_files = true;" $file
  # remove old "option java_package" if any
  sed -i -e '/^option\sjava_package\s=/d' $file
  cmd='grep -Po "^package \K.*(?=;$)" '"${file}"
  # add new "option java_package"
  sed -i -e "/^package\s/a option java_package = \"io.grpc.xds.shaded.$(eval $cmd)\";" $file
done
popd
