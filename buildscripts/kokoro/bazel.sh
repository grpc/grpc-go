#!/bin/bash

set -exu -o pipefail
cat /VERSION

cd github/grpc-java
bazel build ...

cd examples
bazel clean
bazel build ...

cd example-alts
bazel clean
bazel build ...
