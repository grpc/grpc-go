#!/bin/bash
#
#  Copyright 2020 gRPC authors.
#
#  Licensed under the Apache License, Version 2.0 (the "License");
#  you may not use this file except in compliance with the License.
#  You may obtain a copy of the License at
#
#      http://www.apache.org/licenses/LICENSE-2.0
#
#  Unless required by applicable law or agreed to in writing, software
#  distributed under the License is distributed on an "AS IS" BASIS,
#  WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
#  See the License for the specific language governing permissions and
#  limitations under the License.
#

set +e

export TMPDIR=$(mktemp -d)
trap "rm -rf ${TMPDIR}" EXIT

clean () {
  for i in {1..10}; do
    jobs -p | xargs -n1 pkill -P
    # A simple "wait" just hangs sometimes.  Running `jobs` seems to help.
    sleep 1
    if jobs | read; then
      return
    fi
  done
  echo "$(tput setaf 1) clean failed to kill tests $(tput sgr 0)"
  jobs
  pstree
  exit 1
}

fail () {
    echo "$(tput setaf 1) $1 $(tput sgr 0)"
    clean
    exit 1
}

pass () {
    echo "$(tput setaf 2) $1 $(tput sgr 0)"
}

EXAMPLES=(
    "credential_reloading_from_files"
)

declare -A EXPECTED_SERVER_OUTPUT=(
    ["credential_reloading_from_files"]=""
)

declare -A EXPECTED_CLIENT_OUTPUT=(
    ["credential_reloading_from_files"]="Getting message from server: Hello gRPC..."
)

cd ./security/advancedtls/examples

for example in ${EXAMPLES[@]}; do
    echo "$(tput setaf 4) testing: ${example} $(tput sgr 0)"

    # Build server
    if ! go build -o /dev/null ./${example}/*server/*.go; then
        fail "failed to build server"
    else
        pass "successfully built server"
    fi

    # Build client
    if ! go build -o /dev/null ./${example}/*client/*.go; then
        fail "failed to build client"
    else
        pass "successfully built client"
    fi

    # Start server
    SERVER_LOG="$(mktemp)"
    go run ./$example/*server/*.go &> $SERVER_LOG  &

    CLIENT_LOG="$(mktemp)"
    if ! timeout 20 go run ${example}/*client/*.go &> $CLIENT_LOG; then
        fail "client failed to communicate with server
        got server log:
        $(cat $SERVER_LOG)
        got client log:
        $(cat $CLIENT_LOG)
        "
    else
        pass "client successfully communitcated with server"
    fi

    # Check server log for expected output if expecting an
    # output
    if [ -n "${EXPECTED_SERVER_OUTPUT[$example]}" ]; then
        if ! grep -q "${EXPECTED_SERVER_OUTPUT[$example]}" $SERVER_LOG; then
            fail "server log missing output: ${EXPECTED_SERVER_OUTPUT[$example]}
            got server log:
            $(cat $SERVER_LOG)
            got client log:
            $(cat $CLIENT_LOG)
            "
        else
            pass "server log contains expected output: ${EXPECTED_SERVER_OUTPUT[$example]}"
        fi
    fi

    # Check client log for expected output if expecting an
    # output
    if [ -n "${EXPECTED_CLIENT_OUTPUT[$example]}" ]; then
        if ! grep -q "${EXPECTED_CLIENT_OUTPUT[$example]}" $CLIENT_LOG; then
            fail "client log missing output: ${EXPECTED_CLIENT_OUTPUT[$example]}
            got server log:
            $(cat $SERVER_LOG)
            got client log:
            $(cat $CLIENT_LOG)
            "
        else
            pass "client log contains expected output: ${EXPECTED_CLIENT_OUTPUT[$example]}"
        fi
    fi
    clean
    echo ""
done
