#!/bin/bash
# Copyright 2017 The gRPC Authors
#
# Licensed under the Apache License, Version 2.0 (the "License");
# you may not use this file except in compliance with the License.
# You may obtain a copy of the License at
#
#     http://www.apache.org/licenses/LICENSE-2.0
#
# Unless required by applicable law or agreed to in writing, software
# distributed under the License is distributed on an "AS IS" BASIS,
# WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
# See the License for the specific language governing permissions and
# limitations under the License.

set -eu -o pipefail

if [ $# -ne 2 ]; then
  cat <<EOF
Usage: $0 PROFILEID DIR
  PROFILEID The Sonatype profile to use for staging repository
            Obtain profile ID from https://oss.sonatype.org:
             * Build Promotion > Staging Profiles
             * Select profile based on name (e.g., 'io.grpc')
             * Copy hex identifier from URL after "#stagingProfiles;"
  DIR       Directory to upload to Sonatype as a new staging repository

~/.config/sonatype-upload: Configuration file for Sonatype username and password
  USERNAME=yourusername
  PASSWORD=yourpass

Sonatype provides a "user token" that is a randomly generated username/password.
It does allow access to your account, however. You can create one via:
 * Log in to https://oss.sonatype.org
 * Click your username at the top right and click to Profile
 * Change the drop-down from "Summary" to "User Token"
 * Click "Access User Token"
EOF
  exit 1
fi

PROFILE_ID="$1"
DIR="$2"
if [ -z "$DIR" ]; then
  echo "Must specify non-empty directory name"
  exit 1
fi

CONF="$HOME/.config/sonatype-upload"
[ -f "$CONF" ] && . "$CONF"

USERNAME="${USERNAME:-}"
PASSWORD="${PASSWORD:-}"

if [ -z "$USERNAME" -o -z "$PASSWORD" ]; then
  # TODO(ejona86): if people would use it, could prompt for values to avoid
  # having passwords in plain-text.
  echo "You must create '$CONF' with keys USERNAME and PASSWORD" >&2
  exit 1
fi

STAGING_URL="https://oss.sonatype.org/service/local/staging"

# We go through the effort of using deloyByRepositoryId/ because it is
# _substantially_ faster to upload files than deploy/maven2/. When using
# deploy/maven2/ a repository is implicitly created, but the response doesn't
# provide its name.

USERPASS="$USERNAME:$PASSWORD"

# https://oss.sonatype.org/nexus-staging-plugin/default/docs/index.html
#
# Example returned data:
#   <promoteResponse>
#     <data>
#       <stagedRepositoryId>iogrpc-1082</stagedRepositoryId>
#       <description>Release upload</description>
#     </data>
#   </promoteResponse>
echo "Creating staging repo"
REPOID="$(
  XML="
  <promoteRequest>
    <data>
      <description>Release upload</description>
    </data>
  </promoteRequest>"
  curl -s -X POST -d "$XML" -u "$USERPASS" -H "Content-Type: application/xml" \
    "$STAGING_URL/profiles/$PROFILE_ID/start" |
  grep stagedRepositoryId |
  sed 's/.*<stagedRepositoryId>\(.*\)<\/stagedRepositoryId>.*/\1/'
  )"
echo "Repository id: $REPOID"

for X in $(cd "$DIR" && find -type f | cut -b 3-); do
  echo "Uploading $X"
  curl -T "$DIR/$X" -u "$USERPASS" -H "Content-Type: application/octet-stream" \
    "$STAGING_URL/deployByRepositoryId/$REPOID/$X"
done

echo "Closing staging repo"
XML="
<promoteRequest>
  <data>
    <stagedRepositoryId>$REPOID</stagedRepositoryId>
    <description>Auto-close via upload script</description>
  </data>
</promoteRequest>"
curl -X POST -d "$XML" -u "$USERPASS" -H "Content-Type: application/xml" \
  "$STAGING_URL/profiles/$PROFILE_ID/finish"
