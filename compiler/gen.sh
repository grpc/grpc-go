#!/bin/bash

# This script serves as an example to demonstrate how to generate the gRPC-Go
# interface and the related messages.
#
# We suggest the importing paths in proto file are relative to $GOPATH/src and
# this script should be run at $GOPATH/src.
#
# If this is not what you need, feel free to make your own scripts. Again, this
# script is for demonstration purpose.
# 
locProtocGenGo=$1
locGoPlugIn=$2
proto=$3
protoc --plugin=protoc-gen-go=$locProtocGenGo --go_out=. $proto
protoc --plugin=protoc-gen-gogrpc=$locGoPlugIn --gogrpc_out=. $proto
