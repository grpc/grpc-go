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

set -e

if [ $# -ne 1 ]; then
  cat <<EOF
Usage: $0 DIR
  DIR Local repository with files needing to be signed
EOF
  exit 1
fi

find "$1" -type f | egrep -v '\.(md5|sha1)$' | grep -v maven-metadata.xml | xargs gpg -ab
