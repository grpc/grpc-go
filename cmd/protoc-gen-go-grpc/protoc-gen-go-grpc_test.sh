#!/bin/bash
# Copyright 2024 gRPC authors.
#
# Licensed under the Apache License, Version 2.0 (the "License");
# you may not use this file except in compliance with the License.
# You may obtain a copy of the License at
#
#      http://www.apache.org/licenses/LICENSE-2.0
#
# Unless required by applicable law or agreed to in writing, software
# distributed under the License is distributed on an "AS IS" BASIS,
# WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
# See the License for the specific language governing permissions and
# limitations under the License.

# Uncomment to enable debugging.
# set -x 

WORKDIR="$(dirname $0)"
TEMPDIR=$(mktemp -d)

trap "rm -rf ${TEMPDIR}" EXIT

# Build protoc-gen-go-grpc binary and add to $PATH.
pushd "${WORKDIR}"
go build -o "${TEMPDIR}" . 
PATH="${TEMPDIR}:${PATH}"
popd

protoc \
    --go-grpc_out="${TEMPDIR}" \
    --go-grpc_opt=paths=source_relative \
    "$WORKDIR/proto/golden.proto"

GOLDENFILE="${WORKDIR}/testdata/golden_grpc.pb.go"
GENFILE=$(find "${TEMPDIR}" -name "golden_grpc.pb.go")

DIFF=$(diff "${GOLDENFILE}" "${GENFILE}")
if [[ -n "${DIFF}" ]]; then
    echo -e "ERROR: Generated file golden_grpc.pb.go differs from golden file:\n${DIFF}"
    echo -e "If you have made recent changes to protoc-gen-go-grpc," \
     "please regenerate the golden files by running:" \
     "\n\t go generate" >&2
    exit 1
fi

echo SUCCESS
