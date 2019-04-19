#!/bin/bash

# packages is the collection of the packages that are required by xds for grpc.
packages=(
  envoy/service/discovery/v2:ads_go_grpc
  envoy/api/v2:eds_go_grpc
  envoy/api/v2:cds_go_grpc
  envoy/api/v2/core:address_go_proto
  envoy/api/v2/core:base_go_proto
  envoy/api/v2/endpoint:endpoint_go_proto
  envoy/type:percent_go_proto
  envoy/service/load_stats/v2:lrs_go_grpc
)

if [ -z $GOPATH ]; then echo 'empty $GOPATH, exiting.'; exit 1
fi

for i in "${packages[@]}"
do
  (bazel build $i)
done

dest=$GOPATH/src/google.golang.org/grpc/balancer/xds/internal/proto/
rm -rf $dest
srcs=( "$(find -L ./bazel-bin/envoy/ -name *.pb.go)" "$(find -L ./bazel-bin/ -name validate.pb.go)" )
for origin in ${srcs[@]}
do
  target=${origin##*proto/}
  final=$dest$target
  mkdir -p ${final%*/*}
  cp $origin $dest$target
done
