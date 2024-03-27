#!/bin/bash
# Install protoc
echo "Installing protoc"
PROTOC_VERSION="25.2"  # Use quotes for consistency
echo "Protobuf version: ${PROTOC_VERSION}"


download_macos() {
  echo "Attempting to install libprotoc ${PROTOC_VERSION} using Homebrew (macOS)..."
  brew search libprotoc  >/dev/null 2>&1  # Check if Homebrew has the formula
  if [[ $? -eq 0 ]]; then
    PROTOC_VERSION=$(echo "$PROTOC_VERSION/1" | bc)  # Quotes within command substitution
    brew install protobuf@"${PROTOC_VERSION}"
    if [[ $? -eq 0 ]]; then
      echo "libprotoc ${PROTOC_VERSION} successfully installed on macOS"
    else
      echo "Error installing libprotoc on macOS"
    fi
  else
    echo "Could not find libprotoc ${PROTOC_VERSION} formula in Homebrew. Please consider manual installation."
  fi
}


# Function to download pre-built binaries for Linux
download_linux() {
  echo "Attempting to download pre-built libprotoc ${PROTOC_VERSION} binary (Linux)..."

  # Determine architecture
  if [[ $(uname -m) == "x86_64" ]]; then
    ARCH="x86_64"
  elif [[ $(uname -m) == "aarch64" ]]; then
    ARCH="aarch_64"
  else
    echo "Unsupported architecture. Please consider manual installation."
    return
  fi

  # Download URL (adjust if a newer release is available)
  DOWNLOAD_URL="https://github.com/protocolbuffers/protobuf/releases/download/v${PROTOC_VERSION}/protoc-${PROTOC_VERSION}-linux-$ARCH.zip"

  echo "Download URL: ${DOWNLOAD_URL}"

  # Download and unzip
  curl -LO "$DOWNLOAD_URL"  # Quotes around the URL
  echo "Downloaded"
  if [ -z "$WORKDIR" ]; then
      unzip "protoc-${PROTOC_VERSION}-linux-$ARCH.zip"
  else
    unzip "protoc-${PROTOC_VERSION}-linux-$ARCH.zip" -d "${WORKDIR}"  # Quotes around WORKDIR
    rm "protoc-${PROTOC_VERSION}-linux-$ARCH.zip"
  fi
}


download_protoc() {
  # Detect the Operating System
  OS=$(uname -s)

  case "$OS" in  # Quotes around $OS
    "Darwin") download_macos ;;
    "Linux") download_linux ;;
    *) echo "Unsupported operating system. Please consider manual installation." ;;
  esac
}