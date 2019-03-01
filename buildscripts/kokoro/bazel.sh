#!/bin/bash

set -exu -o pipefail
cat /VERSION

use_bazel.sh 0.22.0
bazel version

cd github/grpc-java
bazel build ...

cd examples
bazel clean
bazel build ...
