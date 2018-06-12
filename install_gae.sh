#!/bin/bash

set -ex  # Exit on error; debugging enabled.
set -o pipefail  # Fail a pipe if any sub-command fails.

mkdir /tmp/sdk
curl -o /tmp/sdk.zip "https://storage.googleapis.com/appengine-sdks/featured/go_appengine_sdk_linux_amd64-1.9.64.zip"
unzip -q /tmp/sdk.zip -d /tmp/sdk
export PATH="$PATH:/tmp/sdk/go_appengine"
