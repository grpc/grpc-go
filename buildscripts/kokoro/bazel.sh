#!/bin/bash

set -exu -o pipefail
cat /VERSION
bazel version

cd github/grpc-java
bazel build ...

cd examples
bazel clean
bazel build ...
