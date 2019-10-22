#!/bin/bash
set +e

clean () {
    jobs -p | xargs pkill -P 
    wait
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
    "features/compression"
    "features/deadline"
    "features/encryption/TLS"
)

declare -A EXPECTED_SERVER_OUTPUT=( 
    ["helloworld"]="Received: world"
    ["route_guide"]=""
    ["features/authentication"]="server starting on port 50051..."
    ["features/compression"]="UnaryEcho called with message \"compress\""
    ["features/deadline"]=""
    ["features/encryption/TLS"]=""
)

declare -A EXPECTED_CLIENT_OUTPUT=(
    ["helloworld"]="Greeting: Hello world"
    ["route_guide"]="location:<latitude:416851321 longitude:-742674555 >"
    ["features/authentication"]="UnaryEcho:  hello world"
    ["features/compression"]="UnaryEcho call returned \"compress\", <nil>" 
    ["features/deadline"]="wanted = DeadlineExceeded, got = DeadlineExceeded" 
    ["features/encryption/TLS"]="UnaryEcho:  hello world"
)

for example in ${EXAMPLES[@]}; do
    echo "$(tput setaf 4) testing: ${example} $(tput sgr 0)"

    # Build server
    if ! go build -o /dev/null ./examples/${example}/*server/*.go; then
        fail "failed to build server"
    else
        pass "successfully built server"
    fi

    # Build client
    if ! go build -o /dev/null ./examples/${example}/*client/*.go; then
        fail "failed to build client"
    else
        pass "successfully built client"
    fi
    
    # Start server
    SERVER_LOG="$(mktemp)"
    go run ./examples/$example/*server/*.go &> $SERVER_LOG  &

    CLIENT_LOG="$(mktemp)"
    if ! go run examples/${example}/*client/*.go &> $CLIENT_LOG; then
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
    if [ ! -z "${EXPECTED_SERVER_OUTPUT[$example]}" ]; then
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
    if [ ! -z "${EXPECTED_CLIENT_OUTPUT[$example]}" ]; then
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
done

