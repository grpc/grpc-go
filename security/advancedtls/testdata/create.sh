#!/bin/bash

# Create the client root CA certs (used to verify server identity).
openssl req -x509 -newkey rsa:4096 \
  -keyout client_trust_key_1.pem \
  -out client_trust_cert_1.pem \
  -days 3650 \
  -nodes \
  -subj "/C=US/ST=CA/L=SVL/O=Internet Widgits Pty Ltd" \
  -sha256

openssl req -x509 -newkey rsa:4096 \
  -keyout client_trust_key_2.pem \
  -out client_trust_cert_2.pem \
  -days 3650 \
  -nodes \
  -subj "/C=US/ST=CA/O=Internet Widgits Pty Ltd/CN=foo.bar.client2.trust.com" \
  -sha256

# Create the server root CA certs (used to verify client identity).
openssl req -x509 -newkey rsa:4096 \
  -keyout server_trust_key_1.pem \
  -out server_trust_cert_1.pem \
  -days 3650 \
  -nodes \
  -subj "/C=US/ST=VA/O=Internet Widgits Pty Ltd/CN=foo.bar.hoo.ca.com" \
  -sha256

openssl req -x509 -newkey rsa:4096 \
  -keyout server_trust_key_2.pem \
  -out server_trust_cert_2.pem \
  -days 3650 \
  -nodes \
  -subj "/C=US/ST=CA/O=Internet Widgits Pty Ltd/CN=foo.bar.server2.trust.com" \
  -sha256

# Generate RSA server certificates.
openssl genrsa -out server_key_1.pem 4096
openssl req -new -key server_key_1.pem \
  -subj "/C=US/ST=CA/L=DUMMYCITY/O=Internet Widgits Pty Ltd/CN=foo.bar.com" \
  -out server_csr_1.pem
openssl x509 -req -in server_csr_1.pem \
  -CA client_trust_cert_1.pem -CAkey client_trust_key_1.pem \
  -days 3650 -set_serial 1000 -out server_cert_1.pem -sha256

openssl genrsa -out server_key_2.pem 4096
openssl req -new -key server_key_2.pem \
  -subj "/C=US/ST=CA/O=Internet Widgits Pty Ltd/CN=foo.bar.server2.com" \
  -out server_csr_2.pem
openssl x509 -req -in server_csr_2.pem \
  -CA client_trust_cert_2.pem -CAkey client_trust_key_2.pem \
  -days 3650 -set_serial 1000 -out server_cert_2.pem -sha256

openssl req -x509 -newkey rsa:2048 \
  -keyout server_key_3.pem \
  -out server_cert_3.pem \
  -days 3650 -nodes \
  -subj "/C=US/ST=CA/L=San Jose/O=End Point/OU=Infra/CN=foo.bar.server3.com/emailAddress=cindyxue@google.com" \
  -addext "subjectAltName = DNS:google.com, DNS:apple.com, DNS:amazon.com" \
  -sha256

openssl genrsa -out server_key_localhost_1.pem 4096
openssl req -new -key server_key_localhost_1.pem \
  -subj "/C=US/ST=Illinois/L=Chicago/O=Example, Co./CN=localhost" \
  -config localhost-openssl.cnf -out server_csr_localhost_1.pem
openssl x509 -req -in server_csr_localhost_1.pem \
  -CA client_trust_cert_1.pem -CAkey client_trust_key_1.pem \
  -days 3650 -set_serial 1001 -out server_cert_localhost_1.pem \
  -extfile localhost-openssl.cnf -extensions v3_req -sha256

# Generate ECDSA server certificates.
openssl ecparam -genkey -name prime256v1 -out server_ecdsa_key_1.pem
openssl req -new -key server_ecdsa_key_1.pem \
  -subj "/C=US/ST=CA/L=DUMMYCITY/O=Internet Widgits Pty Ltd/CN=foo.bar.com" \
  -out server_ecdsa_csr_1.pem
openssl x509 -req -in server_ecdsa_csr_1.pem \
  -CA client_trust_cert_1.pem -CAkey client_trust_key_1.pem \
  -days 3650 -set_serial 1002 -out server_ecdsa_cert_1.pem -sha256

openssl ecparam -genkey -name prime256v1 -out server_ecdsa_key_localhost_1.pem
openssl req -new -key server_ecdsa_key_localhost_1.pem \
  -subj "/C=US/ST=Illinois/L=Chicago/O=Example, Co./CN=localhost" \
  -config localhost-openssl.cnf -out server_ecdsa_csr_localhost_1.pem
openssl x509 -req -in server_ecdsa_csr_localhost_1.pem \
  -CA client_trust_cert_1.pem -CAkey client_trust_key_1.pem \
  -days 3650 -set_serial 1003 -out server_ecdsa_cert_localhost_1.pem \
  -extfile localhost-openssl.cnf -extensions v3_req -sha256

# Generate RSA client certificates.
openssl genrsa -out client_key_1.pem 4096
openssl req -new -key client_key_1.pem \
  -subj "/C=US/ST=CA/O=Internet Widgits Pty Ltd/CN=foo.bar.hoo.com" \
  -out client_csr_1.pem
openssl x509 -req -in client_csr_1.pem \
  -CA server_trust_cert_1.pem -CAkey server_trust_key_1.pem \
  -days 3650 -set_serial 1000 -out client_cert_1.pem -sha256

openssl genrsa -out client_key_2.pem 4096
openssl req -new -key client_key_2.pem \
  -subj "/C=US/ST=CA/O=Internet Widgits Pty Ltd/CN=foo.bar.client2.com" \
  -out client_csr_2.pem
openssl x509 -req -in client_csr_2.pem \
  -CA server_trust_cert_2.pem -CAkey server_trust_key_2.pem \
  -days 3650 -set_serial 1000 -out client_cert_2.pem -sha256

openssl genrsa -out another_client_key_1.pem 4096
openssl req -new -key another_client_key_1.pem \
  -subj "/C=US/ST=CA/O=Internet Widgits Pty Ltd/CN=foo.bar.another.client.com" \
  -out another_client_csr_1.pem
openssl x509 -req -in another_client_csr_1.pem \
  -CA server_trust_cert_1.pem -CAkey server_trust_key_1.pem \
  -days 3650 -set_serial 1001 -out another_client_cert_1.pem -sha256

# Generate ECDSA client certificates.
openssl ecparam -genkey -name prime256v1 -out client_ecdsa_key_1.pem
openssl req -new -key client_ecdsa_key_1.pem \
  -subj "/C=US/ST=CA/O=Internet Widgits Pty Ltd/CN=foo.bar.hoo.com" \
  -out client_ecdsa_csr_1.pem
openssl x509 -req -in client_ecdsa_csr_1.pem \
  -CA server_trust_cert_1.pem -CAkey server_trust_key_1.pem \
  -days 3650 -set_serial 1002 -out client_ecdsa_cert_1.pem -sha256

openssl ecparam -genkey -name prime256v1 -out client_ecdsa_key_localhost_1.pem
openssl req -new -key client_ecdsa_key_localhost_1.pem \
  -subj "/C=US/ST=Illinois/L=Chicago/O=Example, Co./CN=localhost" \
  -config localhost-openssl.cnf -out client_ecdsa_csr_localhost_1.pem
openssl x509 -req -in client_ecdsa_csr_localhost_1.pem \
  -CA server_trust_cert_1.pem -CAkey server_trust_key_1.pem \
  -days 3650 -set_serial 1003 -out client_ecdsa_cert_localhost_1.pem \
  -extfile localhost-openssl.cnf -extensions v3_req -sha256

# Verification
openssl verify -verbose -CAfile client_trust_cert_1.pem server_cert_1.pem
openssl verify -verbose -CAfile client_trust_cert_2.pem server_cert_2.pem
openssl verify -verbose -CAfile client_trust_cert_1.pem server_cert_localhost_1.pem
openssl verify -verbose -CAfile client_trust_cert_1.pem server_ecdsa_cert_1.pem
openssl verify -verbose -CAfile client_trust_cert_1.pem server_ecdsa_cert_localhost_1.pem
openssl verify -verbose -CAfile server_trust_cert_1.pem client_cert_1.pem
openssl verify -verbose -CAfile server_trust_cert_2.pem client_cert_2.pem
openssl verify -verbose -CAfile server_trust_cert_1.pem another_client_cert_1.pem
openssl verify -verbose -CAfile server_trust_cert_1.pem client_ecdsa_cert_1.pem
openssl verify -verbose -CAfile server_trust_cert_1.pem client_ecdsa_cert_localhost_1.pem

# Cleanup CSR files.
rm -f *.csr *_csr*.pem
