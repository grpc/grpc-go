#!/bin/bash
# Copyright 2024 gRPC authors.
#
# Licensed under the Apache License, Version 2.0 (the "License");
# you may not use this file except in compliance with the License.
# You may obtain a copy of the License at
#
#      http://www.apache.org/licenses/LICENSE-2.0
#
# Unless required by applicable law or agreed to in writing, software
# distributed under the License is distributed on an "AS IS" BASIS,
# WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
# See the License for the specific language governing permissions and
# limitations under the License.

set -eu -o pipefail

source "$(dirname $0)/vet-common.sh"

# The version of protoc that will be installed.
PROTOC_VERSION="25.2"

# Function to download pre-built binaries for Linux with
# ARCH as $1, OS as $2, and INSTALL_PATH as $3 arguments.
download_binary() {
  # Check if protoc is already available.
  if command -v protoc &> /dev/null; then
      if INSTALL_VERSION=$(protoc --version | cut -d' ' -f2 2>/dev/null); then
        if [ "$INSTALL_VERSION" = "$PROTOC_VERSION" ]; then
          echo "protoc version $PROTOC_VERSION is already installed."
          return
        else
          die "Existing protoc version ($INSTALL_VERSION) differs. Kindly make sure you have $PROTOC_VERSION installed."
        fi
      else
        echo "Unable to determine installed protoc version. Starting the installation."
      fi
  fi
  DOWNLOAD_URL="https://github.com/protocolbuffers/protobuf/releases/download/v${PROTOC_VERSION}/protoc-${PROTOC_VERSION}-$2-$1.zip"
  # Download and unzip
  curl -LO "$DOWNLOAD_URL"
  INSTALL_DIR="${3:-${GOBIN:-${GOPATH:-$HOME/go}/bin}}"
  unzip "protoc-${PROTOC_VERSION}-$2-$1.zip" -d $INSTALL_DIR
  rm "protoc-${PROTOC_VERSION}-$2-$1.zip"
}

# Detect the architecture
case "$(uname -m)" in
  "x86_64") ARCH="x86_64";;
  "aarch64") ARCH="aarch_64";;
  "arm64") ARCH="aarch_64";;
*) die "Unsupported architecture. Please consider manual installation from \
       https://github.com/protocolbuffers/protobuf/releases/ and add to PATH."
esac

# Detect the Operating System
case "$(uname -s)" in
  "Darwin") download_binary $ARCH "osx" "$1";;
  "Linux") download_binary $ARCH "linux" "$1";;
*) die "Unsupported OS. Please consider manual installation from \
   https://github.com/protocolbuffers/protobuf/releases/ and add to PATH" ;;
esac
