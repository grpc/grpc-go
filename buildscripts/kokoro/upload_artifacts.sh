#!/bin/bash
set -veux -o pipefail

if [[ -f /VERSION ]]; then
  cat /VERSION
fi

readonly GRPC_JAVA_DIR="$(cd "$(dirname "$0")"/../.. && pwd)"

echo "Verifying that all artifacts are here..."
find "$KOKORO_GFILE_DIR"

# The output from all the jobs are coalesced into a single dir
LOCAL_MVN_ARTIFACTS="$KOKORO_GFILE_DIR"/github/grpc-java/mvn-artifacts/

# verify that files from all 3 grouped jobs are present.
# platform independent artifacts, from linux job:
[[ "$(find "$LOCAL_MVN_ARTIFACTS" -type f -iname 'grpc-core-*.jar' | wc -l)" != '0' ]]

# android artifact from linux job:
[[ "$(find "$LOCAL_MVN_ARTIFACTS" -type f -iname 'grpc-android-*.aar' | wc -l)" != '0' ]]

# from linux job:
[[ "$(find "$LOCAL_MVN_ARTIFACTS" -type f -iname 'protoc-gen-grpc-java-*-linux-x86_64.exe' | wc -l)" != '0' ]]
[[ "$(find "$LOCAL_MVN_ARTIFACTS" -type f -iname 'protoc-gen-grpc-java-*-linux-x86_32.exe' | wc -l)" != '0' ]]

# from macos job:
[[ "$(find "$LOCAL_MVN_ARTIFACTS" -type f -iname 'protoc-gen-grpc-java-*-osx-x86_64.exe' | wc -l)" != '0' ]]

# from windows job:
[[ "$(find "$LOCAL_MVN_ARTIFACTS" -type f -iname 'protoc-gen-grpc-java-*-windows-x86_64.exe' | wc -l)" != '0' ]]
[[ "$(find "$LOCAL_MVN_ARTIFACTS" -type f -iname 'protoc-gen-grpc-java-*-windows-x86_32.exe' | wc -l)" != '0' ]]


mkdir -p ~/.config/
gsutil cp gs://grpc-testing-secrets/sonatype_credentials/sonatype-upload ~/.config/sonatype-upload

mkdir -p ~/java_signing/
gsutil cp -r gs://grpc-testing-secrets/java_signing/ ~/
gpg --batch  --import ~/java_signing/grpc-java-team-sonatype.asc

# gpg commands changed between v1 and v2 are different.
gpg --version

# This is the version found on kokoro.
if gpg --version | grep 'gpg (GnuPG) 1.'; then
  # This command was tested on 1.4.16
  find "$LOCAL_MVN_ARTIFACTS" -type f \
    -not -name "maven-metadata.xml*" -not -name "*.sha1" -not -name "*.md5" -exec \
    bash -c 'cat ~/java_signing/passphrase | gpg --batch --passphrase-fd 0 --detach-sign -a {}' \;
fi

# This is the version found on corp workstations. Maybe kokoro will be updated to gpg2 some day.
if gpg --version | grep 'gpg (GnuPG) 2.'; then
  # This command was tested on 2.2.2
  find "$LOCAL_MVN_ARTIFACTS" -type f \
    -not -name "maven-metadata.xml*" -not -name "*.sha1" -not -name "*.md5" -exec \
    gpg --batch --passphrase-file ~/java_signing/passphrase --pinentry-mode loopback \
    --detach-sign -a {} \;
fi

STAGING_REPO=a93898609ef848
"$GRPC_JAVA_DIR"/buildscripts/sonatype-upload.sh "$STAGING_REPO" "$LOCAL_MVN_ARTIFACTS"
