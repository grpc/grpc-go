#!/usr/bin/env bash

set -ex
GO_CONTROL_PLANE_VERSION=36c2b082ba77b5785382442ebb13158547ec8f2f

git clone git@github.com:envoyproxy/go-control-plane.git
pushd go-control-plane
git checkout $GO_CONTROL_PLANE_VERSION
popd

rm -rf ./envoy
mv go-control-plane/envoy ./envoy
rm -rf go-control-plane
rm -rf envoy/admin
rm -rf envoy/service/metrics
rm -rf envoy/config/bootstrap
rm -rf envoy/config/trace
rm -rf envoy/service/trace

find ./envoy -type f -name "*.go" -print0 | xargs -0 sed -i 's:github.com/envoyproxy/go-control-plane:google.golang.org/grpc/xds/internal/proto:g'
