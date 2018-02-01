#!/bin/bash

set -exu -o pipefail

cd ./github/grpc-java/cronet
./cronet_deps.sh
../gradlew --include-build .. build
