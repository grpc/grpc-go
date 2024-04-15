#!/bin/bash 

set -ex  # Exit on error; debugging enabled.
set -o pipefail  # Fail a pipe if any sub-command fails.

# - Source them sweet sweet helpers.
source "$(dirname $0)/vet-common.sh"

# - Check to make sure it's safe to modify the user's git repo.
git status --porcelain | fail_on_output

# - Undo any edits made by this script.
cleanup() {
  git reset --hard HEAD
}
trap cleanup EXIT

# - Installs protoc into your ${GOBIN} directory, if requested.
# ($GOBIN might not be the best place for the protoc binary, but is at least
# consistent with the place where all binaries installed by scripts in this repo
# go.)
if [[ "$1" = "-install" ]]; then
  if [[ "${GITHUB_ACTIONS}" = "true" ]]; then
    PROTOBUF_VERSION=25.2 # Shows up in pb.go files as v4.22.0
    PROTOC_FILENAME=protoc-${PROTOBUF_VERSION}-linux-x86_64.zip
    pushd /home/runner/go
      wget https://github.com/google/protobuf/releases/download/v${PROTOBUF_VERSION}/${PROTOC_FILENAME}
      unzip ${PROTOC_FILENAME}
      protoc --version # Check that the binary works.
    popd
  else 
    # TODO: replace with install protoc when https://github.com/grpc/grpc-go/pull/7064 is merged.
    die "-install currently intended for use in CI only."
  fi
  echo SUCCESS
  exit 0
elif [[ "$#" -ne 0 ]]; then
  die "Unknown argument(s): $*"
fi

# - Check that generated proto files are up to date.
go generate google.golang.org/grpc/... && git status --porcelain 2>&1 | fail_on_output || \
(git status; git --no-pager diff; exit 1)

echo SUCCESS
exit 0
