#!/bin/bash
# 
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
#
# install-protoc.sh 
# 
# This script installs the Protocol Buffers compiler (protoc) on the local
# machine. It is used to generate code from .proto files for gRPC communication.
# The script downloads the protoc binary from the official GitHub repository and
# installs it in the system. 
# 
# Usage: ./install-protoc.sh [INSTALL_PATH]
#
# Arguments: 
#   INSTALL_PATH: The path where the protoc binary will be installed (optional).
#                 If not provided, the script will install the binary in the the
#                 directory named by the GOBIN environment variable, which
#                 defaults to $GOPATH/bin or $HOME/go/bin if the GOPATH
#                 environment variable is not set. 
#
# Note: This script requires internet connectivity to download the protoc binary.

set -eu -o pipefail

source "$(dirname $0)/common.sh"

# The version of protoc that will be installed.
PROTOC_VERSION="27.1"

INSTALL_PATH="${1:-${GOBIN:-${GOPATH:-$HOME/go}}}"

# downloads the pre-built binaries for Linux to $INSTALL_PATH.
# Usage:
#   download_binary ARCH OS
# Arguments:
#   ARCH:  The architecture of the system.  Accepted values: [x86_64, aarch_64]
#   OS:    The operating system of the system.  Accepted values: [osx, linux]
download_binary() {  
  DOWNLOAD_URL="https://github.com/protocolbuffers/protobuf/releases/download/v${PROTOC_VERSION}/protoc-${PROTOC_VERSION}-$2-$1.zip"
  # -L follows redirects.
  # -O writes output to a file.
  curl -LO "${DOWNLOAD_URL}"
  unzip "protoc-${PROTOC_VERSION}-$2-$1.zip" -d "${INSTALL_PATH}"
  rm "protoc-${PROTOC_VERSION}-$2-$1.zip"
  rm "${INSTALL_PATH}/readme.txt"
}

main() {
  # Check if protoc is already available.
  if command -v protoc &> /dev/null; then
    if INSTALL_VERSION=$(protoc --version | cut -d' ' -f2 2>/dev/null); then
      if [ "$INSTALL_VERSION" = "$PROTOC_VERSION" ]; then
        echo "protoc version $PROTOC_VERSION is already installed."
        return
      else
        die "Existing protoc version ($INSTALL_VERSION) differs. Kindly make sure you have $PROTOC_VERSION installed."
      fi
    fi
  fi

  # Detect the architecture
  case "$(uname -m)" in
    "x86_64") ARCH="x86_64";;
    "aarch64") ARCH="aarch_64";;
    "arm64") ARCH="aarch_64";;
  *) die "Unsupported architecture. Please consider manual installation from "\
          "https://github.com/protocolbuffers/protobuf/releases/ and add to PATH."
  esac

  # Detect the Operating System
  case "$(uname -s)" in
    "Darwin") download_binary $ARCH "osx";;
    "Linux") download_binary $ARCH "linux";;
  *) die "Unsupported OS. Please consider manual installation from "\
          "https://github.com/protocolbuffers/protobuf/releases/ and add to PATH" ;;
  esac
}

main "$@"
