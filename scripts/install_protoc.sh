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

# Perform installation of protoc from source based on OS.

PROTOC_VERSION="25.2"

# Function to download pre-built binaries for Linux with
# ARCH as $1, OS as $2, and WORKDIR as $3 arguments.
download_binary() {
  # Check if protoc is already available
  if command -v protoc &> /dev/null; then
      if installed_version=$(protoc --version | cut -d' ' -f2 2>/dev/null); then
        if [ "$installed_version" = "$PROTOC_VERSION" ]; then
          echo "protoc version $PROTOC_VERSION is already installed."
          return
        else
          echo "Existing protoc version ($installed_version) differs. Kindly make sure you have $PROTOC_VERSION installed."
#          exit 1
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

# Determine architecture
if [[ $(uname -m) == "x86_64" ]]; then
  ARCH="x86_64"
elif [[ $(uname -m) == "aarch64" ]] || [[ $(uname -m) == "arm64" ]] ; then
  ARCH="aarch_64"
else
  die "Unsupported architecture. Please consider manual installation."
fi
# Detect the Operating System
case "$(uname -s)" in
  "Darwin") download_binary $ARCH "osx" "$1";;
  "Linux") download_binary $ARCH "linux" "$1";;
*) echo "Please consider manual installation from \
   https://github.com/protocolbuffers/protobuf/releases/ and add to PATH" ;;
esac
