#!/bin/bash

# Please run this under the same directory where the input proto stays. The
# output will stay in the same directory. If this is not the behavior you want,
# feel free to make your own scripts.
locProtocGenGo=$1
locGoPlugIn=$2
proto=$3
protoc --plugin=protoc-gen-go=$locProtocGenGo --go_out=. $proto
protoc --plugin=protoc-gen-gogrpc=$locGoPlugIn --gogrpc_out=. $proto
