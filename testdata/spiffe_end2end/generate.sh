#!/bin/bash

# Generate client/server self signed CAs and certs.
openssl req -x509 -newkey rsa:4096 -keyout ca.key -out ca.pem -days 365 -nodes -subj "/C=US/ST=VA/O=Internet Widgits Pty Ltd/CN=foo.bar.hoo.ca.com"

# The SPIFFE related extensions are listed in spiffe-openssl.cnf config. Both
# client_spiffe.pem and server_spiffe.pem are generated in the same way with
# original client.pem and server.pem but with using that config. Here are the
# exact commands (we pass "-subj" as argument in this case):
openssl genrsa -out client.key.rsa 2048
openssl pkcs8 -topk8 -in client.key.rsa -out client.key -nocrypt
openssl req -new -key client.key -out spiffe-cert.csr \
 -subj /C=US/ST=CA/L=SVL/O=gRPC/CN=testclient/ \
 -config spiffe-openssl.cnf -reqexts spiffe_client_e2e
openssl x509 -req -CA ca.pem -CAkey ca.key -CAcreateserial \
 -in spiffe-cert.csr -out client_spiffe.pem -extensions spiffe_client_e2e \
  -extfile spiffe-openssl.cnf -days 3650 -sha256

openssl genrsa -out server.key.rsa 2048
openssl pkcs8 -topk8 -in server.key.rsa -out server.key -nocrypt
openssl req -new -key server.key -out spiffe-cert.csr \
 -subj "/C=US/ST=CA/L=SVL/O=gRPC/CN=*.test.google.com/" \
 -config spiffe-openssl.cnf -reqexts spiffe_server_e2e
openssl x509 -req -CA ca.pem -CAkey ca.key -CAcreateserial \
 -in spiffe-cert.csr -out server_spiffe.pem -extensions spiffe_server_e2e \
  -extfile spiffe-openssl.cnf -days 3650 -sha256

rm *.rsa
rm *.csr
rm *.srl
