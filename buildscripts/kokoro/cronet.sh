#!/bin/bash

set -exu -o pipefail

cd ./github/grpc-java
./gradlew -PskipCodegen=true install

cd cronet
./cronet_deps.sh
../gradlew build
