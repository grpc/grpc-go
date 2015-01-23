#!/bin/bash

shopt -s globstar

origin='github.com/google/grpc-go/metadata'
replace='github.com/google/grpc-go/rpc/metadata'
sed -i "s%$origin%$replace%g" **/*.go

