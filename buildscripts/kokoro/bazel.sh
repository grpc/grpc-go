#!/bin/bash

set -exu -o pipefail

cd github/grpc-java
bazel build ...
