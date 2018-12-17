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
# import VERSION from one of the google internal CLs
VERSION=6ea4a035315109fa6c5eff5b74e983729a179f3f
GIT_REPO="https://github.com/envoyproxy/envoy.git"
GIT_BASE_DIR=envoy
SOURCE_PROTO_BASE_DIR=envoy/api
TARGET_PROTO_BASE_DIR=src/main/proto
FILES=(
envoy/api/v2/auth/cert.proto
envoy/api/v2/cds.proto
envoy/api/v2/cluster/circuit_breaker.proto
envoy/api/v2/cluster/outlier_detection.proto
envoy/api/v2/core/address.proto
envoy/api/v2/core/base.proto
envoy/api/v2/core/config_source.proto
envoy/api/v2/core/grpc_service.proto
envoy/api/v2/core/health_check.proto
envoy/api/v2/core/protocol.proto
envoy/api/v2/discovery.proto
envoy/api/v2/eds.proto
envoy/api/v2/endpoint/endpoint.proto
envoy/api/v2/endpoint/load_report.proto
envoy/type/percent.proto
)

# clone the envoy github repo in /tmp directory
pushd /tmp
rm -rf $GIT_BASE_DIR
git clone -b $BRANCH $GIT_REPO
cd $GIT_BASE_DIR
git checkout $VERSION
popd

cp -p /tmp/${GIT_BASE_DIR}/LICENSE LICENSE
cp -p /tmp/${GIT_BASE_DIR}/NOTICE NOTICE

mkdir -p ${TARGET_PROTO_BASE_DIR}
pushd ${TARGET_PROTO_BASE_DIR}

# copy proto files to project directory
for file in "${FILES[@]}"
do
  mkdir -p $(dirname ${file})
  cp -p /tmp/${SOURCE_PROTO_BASE_DIR}/${file} ${file}
done

# See google internal third_party/envoy/envoy-update.sh
# ===========================================================================
# Fix up proto imports and remove references to gogoproto.
# ===========================================================================
for f in "${FILES[@]}"
do
  commands=(
    # Import mangling.
    -e 's#import "gogoproto/gogo.proto";##'
    # Remove references to gogo.proto extensions.
    -e 's#option (gogoproto\.[a-z_]\+) = \(true\|false\);##'
    -e 's#\(, \)\?(gogoproto\.[a-z_]\+) = \(true\|false\),\?##'
    # gogoproto removal can result in empty brackets.
    -e 's# \[\]##'
    # gogoproto removal can result in four spaces on a line by itself.
    -e '/^    $/d'
  )
  sed -i "${commands[@]}" "$f"

  # gogoproto removal can leave a comma on the last element in a list.
  # This needs to run separately after all the commands above have finished
  # since it is multi-line and rewrites the output of the above patterns.
  sed -i -e '$!N; s#\(.*\),\([[:space:]]*\];\)#\1\2#; t; P; D;' "$f"
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
