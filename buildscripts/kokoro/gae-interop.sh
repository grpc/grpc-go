#!/bin/bash

set -exu -o pipefail
if [[ -f /VERSION ]]; then
  cat /VERSION
fi

KOKORO_GAE_SERVICE="java-gae-interop-test"

# We deploy as different versions of a single service, this way any stale
# lingering deploys can be easily cleaned up by purging all running versions
# of this service.
KOKORO_GAE_APP_VERSION=$(hostname)

# A dummy version that can be the recipient of all traffic, so that the kokoro test version can be
# set to 0 traffic. This is a requirement in order to delete it.
DUMMY_DEFAULT_VERSION='dummy-default'

function cleanup() {
  echo "Performing cleanup now."
  gcloud app services delete $KOKORO_GAE_SERVICE --version $KOKORO_GAE_APP_VERSION --quiet
}
trap cleanup SIGHUP SIGINT SIGTERM EXIT

readonly GRPC_JAVA_DIR="$(cd "$(dirname "$0")"/../.. && pwd)"
cd "$GRPC_JAVA_DIR"

##
## Deploy the dummy 'default' version of the service
##
GRADLE_FLAGS="--stacktrace -DgaeStopPreviousVersion=false -PskipCodegen=true"

# Deploy the dummy 'default' version. We only require that it exists when cleanup() is called.
# It ok if we race with another run and fail here, because the end result is idempotent.
set +e
if ! gcloud app versions describe "$DUMMY_DEFAULT_VERSION" --service="$KOKORO_GAE_SERVICE"; then
  ./gradlew $GRADLE_FLAGS -DgaeDeployVersion="$DUMMY_DEFAULT_VERSION" -DgaePromote=true :grpc-gae-interop-testing-jdk8:appengineDeploy
else
  echo "default version already exists: $DUMMY_DEFAULT_VERSION"
fi
set -e

# Deploy and test the real app (jdk8)
./gradlew $GRADLE_FLAGS -DgaeDeployVersion="$KOKORO_GAE_APP_VERSION" :grpc-gae-interop-testing-jdk8:runInteropTestRemote

set +e
echo "Cleaning out stale deploys from previous runs, it is ok if this part fails"

# Sometimes the trap based cleanup fails.
# Delete all versions whose name is not 'dummy-default' and is older than 1 hour.
# This expression is an ISO8601 relative date:
# https://cloud.google.com/sdk/gcloud/reference/topic/datetimes
gcloud app versions list --format="get(version.id)" --filter="service=$KOKORO_GAE_SERVICE AND NOT version : 'dummy-default' AND version.createTime<'-p1h'" | xargs -i gcloud app services delete "$KOKORO_GAE_SERVICE" --version {} --quiet
exit 0
