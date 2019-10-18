#!/bin/bash
 
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

clean () {
    pkill -s 0
    wait
}

fail () {
    echo "$(tput setaf 1) $1 $(tput sgr 0)"
    clean
    exit 1
}

pass () {
    echo $"$(tput setaf 2) $1 $(tput sgr 0)"
}

# Build greeter server
if ! go build -o /dev/null ./examples/helloworld/greeter_server/*.go; then
    fail "failed to build greeter server"
else
    pass "successfully built greeter server"
fi

# Build greeter client
if ! go build -o /dev/null ./examples/helloworld/greeter_client/*.go; then
    fail "failed to build greeter client"
else
    pass "successfully built greeter client"
fi

# Server should be able to start
SERVER_LOG="$(mktemp)"
go run examples/helloworld/greeter_server/*.go &> $SERVER_LOG & 

# Client should be able to communicate to the active server
CLIENT_LOG="$(mktemp)"
if ! go run examples/helloworld/greeter_client/*.go &> $CLIENT_LOG; then
    fail "client failed to communicate with server
    Got server log:
    $(cat $SERVER_LOG)
    Got client log:
    $(cat $CLIENT_LOG)
    "
else
    pass "client successfully communicated with server"
fi

# Out log should contain the string 'Received: world'
# from the server.
if ! grep -q "Received: world" $SERVER_LOG; then
    fail "Server log missing server output: 'Received: world'
    Got server log:
    $(cat $SERVER_LOG)
    Got client log:
    $(cat $CLIENT_LOG)
    "
else
    pass "Server log contains server output: 'Received: world'"
fi

# Out log should contain the string 'Greeting: Hello world'
# from the client.
if ! grep -q "Greeting: Hello world" $CLIENT_LOG ; then
    fail "Client log missing client output: 'Greeting: Hello world'
    Got server log:
    $(cat $SERVER_LOG)
    Got client log:
    $(cat $CLIENT_LOG)
    "
else
    pass "Client log contains client output: 'Greeting: Hello world'"
fi

clean
exit 0

