#!/bin/bash

set -exu -o pipefail

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
  gcloud app services set-traffic $KOKORO_GAE_SERVICE --quiet --splits $DUMMY_DEFAULT_VERSION=1.0
  gcloud app services delete $KOKORO_GAE_SERVICE --version $KOKORO_GAE_APP_VERSION --quiet
}
trap cleanup SIGHUP SIGINT SIGTERM EXIT

cd ./github/grpc-java

##
## Deploy the dummy 'default' version of the service
##
echo "<?xml version='1.0' encoding='utf-8'?>
<appengine-web-app xmlns='http://appengine.google.com/ns/1.0'>
  <threadsafe>true</threadsafe>
  <service>$KOKORO_GAE_SERVICE</service>
  <runtime>java8</runtime>
</appengine-web-app>
" > ./gae-interop-testing/gae-jdk8/src/main/webapp/WEB-INF/appengine-web.xml
cat ./gae-interop-testing/gae-jdk8/src/main/webapp/WEB-INF/appengine-web.xml

GRADLE_FLAGS="--stacktrace -DgaeStopPreviousVersion=false -DgaePromote=false -PskipCodegen=true"
DUMMY_EXISTS_CMD="gcloud app versions describe $DUMMY_DEFAULT_VERSION --service=$KOKORO_GAE_SERVICE"
DEPLOY_DUMMY_CMD="./gradlew $GRADLE_FLAGS -DgaeDeployVersion=$DUMMY_DEFAULT_VERSION :grpc-gae-interop-testing-jdk8:appengineDeploy"

# Deploy the dummy 'default' version. We only require that it exists when cleanup() is called.
set +e
$DUMMY_EXISTS_CMD
set -e

if [[ $? != 0 ]]; then
  $DEPLOY_DUMMY_CMD
else
  echo "default version already exists: $DUMMY_DEFAULT_VERSION"
fi

##
## Begin JDK8 test
##
echo "<?xml version='1.0' encoding='utf-8'?>
<appengine-web-app xmlns='http://appengine.google.com/ns/1.0'>
  <threadsafe>true</threadsafe>
  <service>$KOKORO_GAE_SERVICE</service>
  <runtime>java8</runtime>
</appengine-web-app>
" > ./gae-interop-testing/gae-jdk8/src/main/webapp/WEB-INF/appengine-web.xml
cat ./gae-interop-testing/gae-jdk8/src/main/webapp/WEB-INF/appengine-web.xml
# Deploy and test the real app (jdk8)
./gradlew $GRADLE_FLAGS -DgaeDeployVersion=$KOKORO_GAE_APP_VERSION :grpc-gae-interop-testing-jdk8:runInteropTestRemote

##
## Begin JDK7 test
##
echo "<?xml version='1.0' encoding='utf-8'?>
<appengine-web-app xmlns='http://appengine.google.com/ns/1.0'>
  <threadsafe>true</threadsafe>
  <service>$KOKORO_GAE_SERVICE</service>
  <runtime>java7</runtime>
</appengine-web-app>
" > ./gae-interop-testing/gae-jdk7/src/main/webapp/WEB-INF/appengine-web.xml
cat ./gae-interop-testing/gae-jdk7/src/main/webapp/WEB-INF/appengine-web.xml
# Deploy and test the real app (jdk7)
./gradlew $GRADLE_FLAGS -DgaeDeployVersion=$KOKORO_GAE_APP_VERSION :grpc-gae-interop-testing-jdk7:runInteropTestRemote

set +e
echo "Cleaning out stale deploys from previous runs, it is ok if this part fails"

# Sometimes the trap based cleanup fails.
# Delete all versions older than 1 hour. This expression is an ISO8601 relative date:
# https://cloud.google.com/sdk/gcloud/reference/topic/datetimes
gcloud app versions list --format="get(version.id)" --filter="service=$KOKORO_GAE_SERVICE AND version.createTime<'-p1h'" | xargs -i gcloud app services delete $KOKORO_GAE_SERVICE --version {} --quiet
exit 0
