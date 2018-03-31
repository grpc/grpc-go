#!/bin/bash

set -exu -o pipefail
cat /VERSION

cd github/grpc-java
bazel build ...

cd examples
bazel build ...
