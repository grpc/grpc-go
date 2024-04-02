#!/bin/bash
# Install protoc
echo "Installing protoc"
PROTOC_VERSION="25.2"
echo "Protobuf version: ${PROTOC_VERSION}"

# Function to download pre-built binaries for Linux
download_binary() {
  echo "Attempting to download pre-built libprotoc ${PROTOC_VERSION} binary."

  # Download URL (adjust if a newer release is available)
  DOWNLOAD_URL="https://github.com/protocolbuffers/protobuf/releases/download/v${PROTOC_VERSION}/protoc-${PROTOC_VERSION}-$2-$1.zip"
  echo "Download URL: ${DOWNLOAD_URL}"

  # Download and unzip
  curl -LO "$DOWNLOAD_URL"
  echo "Downloaded"
  INSTALL_DIR="${3:-/home/runner/go}"
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
      echo "Unsupported architecture. Please consider manual installation."
      return
    fi
  # Detect the Operating System
  OS=$(uname -s)
  case "$OS" in
    "Darwin") download_binary $ARCH "osx" "${WORKDIR:-.}";;
    "Linux") download_binary $ARCH "linux" $1;;
    *) echo "Unsupported operating system. Please consider manual installation." ;;
  esac
}
