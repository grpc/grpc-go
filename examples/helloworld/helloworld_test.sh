#!/bin/bash

set +e

clean () {
    if [ ! -z $PID1 ]; then
        kill $PID1
        wait $PID1 2>/dev/null
    fi

    rm -f greeter_server/server
    rm -f greeter_client/client
    
    rm out.log
}


fail () {
    echo "$(tput setaf 1) $1 $(tput sgr 0)"
    clean
    exit 1
}

pass () {
    echo "$(tput setaf 2) $1 $(tput sgr 0)"
}


# Build greeter server
cd greeter_server && go build -o server &>/dev/null
if [ $? -ne 0 ]; then
    fail "failed to build greeter server"
else
    pass "successfully built greeter server"

fi

# Build greeter client
cd ../greeter_client && go build -o client &>/dev/null
if [ $? -ne 0 ]; then
    fail "failed to build greeter client"
else
    pass "successfully built greeter client"
fi

# Client should throw an error if it cannot connect to a server
./client &>/dev/null
if [ $? == 0 ]; then
    fail "client communicated when there should be no active server"
else
    pass "client failed to communicate when there is no active server"
fi

# Return to helloworld directory
cd ..

# Server should be able to start
./greeter_server/server &> out.log &
PID1=$!
if [ $? -ne 0 ]; then
    fail "failed to start greeter server"
else
    pass "successfully started greeter server"
fi

# Client should be able to communicate to the active server
./greeter_client/client &>> out.log
if [ $? -ne 0 ]; then
    fail "client failed to communicate with server"
else
    pass "client successfully communicated with server"
fi

# Out log should contain the string 'Received: world'
# from the server.
server_output=`grep "Received: world" out.log`
if [ -z "$server_output" ]; then
    fail "out log missing server output: 'Received: world'"
else
    pass "out log contains server output: 'Received: world'"
fi

# Out log should contain the string 'Greeting: Hello world'
# from the client.
client_output=`grep "Greeting: Hello world" out.log`
if [ -z "$client_output" ]; then
    fail "out log missing client output: 'Greeting: Hello world'"
else
    pass "out log contains client output: 'Greeting: Hello world'"
fi

clean
