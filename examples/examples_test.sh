#!/bin/bash
#
#  Copyright 2019 gRPC authors.
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

export SERVER_PORT=50051
export UNIX_ADDR=abstract-unix-socket

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
    "helloworld"
    "route_guide"
    "features/authentication"
    "features/authz"
    "features/cancellation"
    "features/compression"
    "features/deadline"
    "features/encryption/TLS"
    "features/error_details"
    "features/error_handling"
    "features/flow_control"
    "features/interceptor"
    "features/load_balancing"
    "features/metadata"
    "features/metadata_interceptor"
    "features/multiplex"
    "features/name_resolving"
    "features/orca"
    "features/retry"
    "features/unix_abstract"
)

declare -A SERVER_ARGS=(
    ["features/unix_abstract"]="-addr $UNIX_ADDR"
    ["default"]="-port $SERVER_PORT"
)

declare -A CLIENT_ARGS=(
    ["features/unix_abstract"]="-addr $UNIX_ADDR"
    ["features/orca"]="-test=true"
    ["default"]="-addr localhost:$SERVER_PORT"
)

declare -A SERVER_WAIT_COMMAND=(
    ["features/unix_abstract"]="lsof -U | grep $UNIX_ADDR"
    ["default"]="lsof -i :$SERVER_PORT | grep $SERVER_PORT"
)

wait_for_server () {
    example=$1
    wait_command=${SERVER_WAIT_COMMAND[$example]:-${SERVER_WAIT_COMMAND["default"]}}
    echo "$(tput setaf 4) waiting for server to start $(tput sgr 0)"
    for i in {1..10}; do
        eval "$wait_command" 2>&1 &>/dev/null
        if [ $? -eq 0 ]; then
            pass "server started"
            return
        fi
        sleep 1
    done
    fail "cannot determine if server started"
}

declare -A EXPECTED_SERVER_OUTPUT=(
    ["helloworld"]="Received: world"
    ["route_guide"]=""
    ["features/authentication"]="server starting on port 50051..."
    ["features/authz"]="unary echoing message \"hello world\""
    ["features/cancellation"]="server: error receiving from stream: rpc error: code = Canceled desc = context canceled"
    ["features/compression"]="UnaryEcho called with message \"compress\""
    ["features/deadline"]=""
    ["features/encryption/TLS"]=""
    ["features/error_details"]=""
    ["features/error_handling"]=""
    ["features/flow_control"]="Stream ended successfully."
    ["features/interceptor"]="unary echoing message \"hello world\""
    ["features/load_balancing"]="serving on :50051"
    ["features/metadata"]="message:\"this is examples/metadata\", sending echo"
    ["features/metadata_interceptor"]="key1 from metadata: "
    ["features/multiplex"]=":50051"
    ["features/name_resolving"]="serving on localhost:50051"
    ["features/orca"]="Server listening"
    ["features/retry"]="request succeeded count: 4"
    ["features/unix_abstract"]="serving on @abstract-unix-socket"
)

declare -A EXPECTED_CLIENT_OUTPUT=(
    ["helloworld"]="Greeting: Hello world"
    ["route_guide"]="Feature: name: \"\", point:(416851321, -742674555)"
    ["features/authentication"]="UnaryEcho:  hello world"
    ["features/authz"]="UnaryEcho:  hello world"
    ["features/cancellation"]="cancelling context"
    ["features/compression"]="UnaryEcho call returned \"compress\", <nil>"
    ["features/deadline"]="wanted = DeadlineExceeded, got = DeadlineExceeded"
    ["features/encryption/TLS"]="UnaryEcho:  hello world"
    ["features/error_details"]="Greeting: Hello world"
    ["features/error_handling"]="Received error"
    ["features/flow_control"]="Stream ended successfully."
    ["features/interceptor"]="UnaryEcho:  hello world"
    ["features/load_balancing"]="calling helloworld.Greeter/SayHello with pick_first"
    ["features/metadata"]="this is examples/metadata"
    ["features/metadata_interceptor"]="BidiStreaming Echo:  hello world"
    ["features/multiplex"]="Greeting:  Hello multiplex"
    ["features/name_resolving"]="calling helloworld.Greeter/SayHello to \"example:///resolver.example.grpc.io\""
    ["features/orca"]="Per-call load report received: map\[db_queries:10\]"
    ["features/retry"]="UnaryEcho reply: message:\"Try and Success\""
    ["features/unix_abstract"]="calling echo.Echo/UnaryEcho to unix-abstract:abstract-unix-socket"
)

cd ./examples

for example in ${EXAMPLES[@]}; do
    echo "$(tput setaf 4) testing: ${example} $(tput sgr 0)"

    # Build server
    if ! go build -o /dev/null ./${example}/*server/*.go; then
        fail "failed to build server"
    else
        pass "successfully built server"
    fi

    # Start server
    SERVER_LOG="$(mktemp)"
    server_args=${SERVER_ARGS[$example]:-${SERVER_ARGS["default"]}}
    go run ./$example/*server/*.go $server_args &> $SERVER_LOG  &

    wait_for_server $example

    # Build client
    if ! go build -o /dev/null ./${example}/*client/*.go; then
        fail "failed to build client"
    else
        pass "successfully built client"
    fi

    # Start client
    CLIENT_LOG="$(mktemp)"
    client_args=${CLIENT_ARGS[$example]:-${CLIENT_ARGS["default"]}}
    if ! timeout 20 go run ${example}/*client/*.go $client_args &> $CLIENT_LOG; then
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
