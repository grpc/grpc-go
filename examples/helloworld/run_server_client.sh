#!/bin/bash
# This script will start greeter server and using greeter client to send a grpc call

# Compile executable server and client
cd greeter_server && go build -o ../server
cd ../greeter_client && go build -o ../client
cd ..

# Run server
./server & SERVER_PID=$!
# Wait 0.5s to ensure server is started then run client
sleep 0.5 & ./client

# Clean up after client sent request
kill $SERVER_PID
rm -f server; rm -f client
