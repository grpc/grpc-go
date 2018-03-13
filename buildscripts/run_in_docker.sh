#!/bin/bash
set -eu -o pipefail

quote() {
  local arg
  for arg in "$@"; do
    printf "'"
    printf "%s" "$arg" | sed -e "s/'/'\\\\''/g"
    printf "' "
  done
}

if [[ "${1:-}" != "in-docker" ]]; then
  readonly grpc_java_dir="$(dirname $(readlink -f "$0"))/.."
  exec docker run -it --rm=true -v "${grpc_java_dir}:/grpc-java" -w /grpc-java \
    grpc-java-releasing \
    ./buildscripts/run_in_docker.sh in-docker "$(id -u)" "$(id -g)" "$@"
fi

## In Docker

shift

readonly swap_uid="$1"
readonly swap_gid="$2"
shift 2

# Java uses NSS to determine the user's home. If that fails it uses '?' in the
# current directory. So we need to set up the user's home in /etc/passwd.
# If this wasn't the case, we could have passed -u to docker run and avoided
# this script inside the container. JAVA_TOOL_OPTIONS is okay, but is noisy.
groupadd thegroup -g "$swap_gid"
useradd theuser -u "$swap_uid" -g "$swap_gid" -m
if [[ "$#" -eq 0 ]]; then
  exec su theuser
else
  # runuser is too old in the container to support the -u flag; if it did, we'd
  # be able to remove the 'quote' function.
  exec su theuser -c "$(quote "$@")"
fi
