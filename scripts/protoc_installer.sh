#!/bin/bash
# Install protoc
PROTOC_VERSION="25.2"

# Function to download pre-built binaries for Linux
download_binary() {
  # Download URL (adjust if a newer release is available)
  DOWNLOAD_URL="https://github.com/protocolbuffers/protobuf/releases/download/v${PROTOC_VERSION}/protoc-${PROTOC_VERSION}-$2-$1.zip"
  # Download and unzip
  curl -LO "$DOWNLOAD_URL"
  INSTALL_DIR="${3:-${GOBIN:-${GOPATH:-$HOME/go}/bin}}"
  unzip "protoc-${PROTOC_VERSION}-$2-$1.zip" -d $INSTALL_DIR
  rm "protoc-${PROTOC_VERSION}-$2-$1.zip"
}

download_protoc() {
  # Determine architecture
  if [[ $(uname -m) == "x86_64" ]]; then
    ARCH="x86_64"
  elif [[ $(uname -m) == "aarch64" ]] || [[ $(uname -m) == "arm64" ]] ; then
    ARCH="aarch_64"
  else
    die "Unsupported architecture. Please consider manual installation."
  fi
  # Detect the Operating System
  OS=$(uname -s)
  case "$OS" in
    "Darwin") download_binary $ARCH "osx" "$1";;
    "Linux") download_binary $ARCH "linux" $1;;
  *) echo "Unsupported operating system. Please consider manual installation." ;;
  esac
}
