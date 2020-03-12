#!/bin/bash

set -exu -o pipefail
if [[ -f /VERSION ]]; then
  cat /VERSION
fi

cd github

export GOPATH=~/gopath
REPO_ROOT=${GOPATH}/src/google.golang.org/grpc

mkdir -p ${GOPATH}/src/google.golang.org
mv grpc-go/ ${REPO_ROOT}
pushd ${REPO_ROOT}
go get -v ./...
go build ...grpc
pushd interop/xds/client
go build
popd
popd

git clone https://github.com/grpc/grpc.git

grpc/tools/run_tests/helper_scripts/prep_xds.sh
GRPC_GO_LOG_VERBOSITY_LEVEL=99 GRPC_GO_LOG_SEVERITY_LEVEL=info \
  python3 grpc/tools/run_tests/run_xds_tests.py \
    --test_case=all \
    --project_id=grpc-testing \
    --gcp_suffix=$(date '+%s') \
    --verbose \
    --client_cmd="${REPO_ROOT}/interop/xds/client/client \
      --server=xds-experimental:///{server_uri} \
      --stats_port={stats_port} \
      --qps={qps}"
