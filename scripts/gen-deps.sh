#!/bin/bash

set -e          # Exit on error
set -o pipefail # Fail a pipe if any sub-command fails.

source "$(dirname $0)/common.sh"

if [[ "$#" -ne 1 || ! -d "$1" ]]; then
    echo "Specify a valid output directory as the first parameter."
    exit 1
fi

SCRIPTS_DIR="$(dirname "$0")"
OUTPUT_DIR="$1"

cd "${SCRIPTS_DIR}/.."

git ls-files -- '*.go' | grep -v '\(^\|/\)\(internal\|examples\|benchmark\|interop\|test\|testdata\)\(/\|$\)' | xargs dirname | sort -u | while read d; do
  pushd "$d" > /dev/null
  pkg="$(echo "$d" | sed 's;\.;grpc;' | sed 's;/;_;g')"
  go list -deps . | sort | noret_grep -v 'google.golang.org/grpc' >| "${OUTPUT_DIR}/$pkg"
  popd > /dev/null
done
