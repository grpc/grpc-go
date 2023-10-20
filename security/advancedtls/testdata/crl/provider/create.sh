#!/bin/bash

# The script contains a sequence of commands described in README.md
openssl req -x509                                                      \
  -newkey rsa:4096                                                     \
  -keyout server_trust_key.pem                                         \
  -out server_trust_cert.pem                                           \
  -days 365                                                            \
  -subj "/C=US/ST=VA/O=Internet Widgits Pty Ltd/CN=foo.bar.hoo.ca.com" \
  -nodes

openssl req -x509                                      \
  -newkey rsa:4096                                     \
  -keyout client_trust_key.pem                         \
  -out client_trust_cert.pem                           \
  -days 365                                            \
  -subj "/C=US/ST=CA/L=SVL/O=Internet Widgits Pty Ltd" \
  -nodes

openssl req -newkey rsa:4096                                                \
  -keyout server_cert.key                                                   \
  -out new_cert.csr                                                         \
  -nodes                                                                    \
  -subj "/C=US/ST=CA/L=DUMMYCITY/O=Internet Widgits Pty Ltd/CN=foo.bar.com" \
  -sha256

openssl x509 -req             \
  -in new_cert.csr            \
  -out server_cert.pem        \
  -CA client_trust_cert.pem   \
  -CAkey client_trust_key.pem \
  -CAcreateserial             \
  -days 3650                  \
  -sha256                     \
  -extfile extensions.conf

openssl req -newkey rsa:4096                                        \
  -keyout client_cert.key                                           \
  -out new_cert.csr                                                 \
  -nodes                                                            \
  -subj "/C=US/ST=CA/O=Internet Widgits Pty Ltd/CN=foo.bar.hoo.com" \
  -sha256

openssl x509 -req             \
  -in new_cert.csr            \
  -out client_cert.pem        \
  -CA server_trust_cert.pem   \
  -CAkey server_trust_key.pem \
  -CAcreateserial             \
  -days 3650                  \
  -sha256                     \
  -extfile extensions.conf

echo "1000" > crlnumber.txt

touch index.txt

openssl ca -gencrl              \
  -keyfile client_trust_key.pem \
  -cert client_trust_cert.pem   \
  -out crl_empty.pem            \
  -config crl.cnf

openssl ca -revoke server_cert.pem \
  -keyfile client_trust_key.pem    \
  -cert client_trust_cert.pem      \
  -config crl.cnf

openssl ca -gencrl              \
  -keyfile client_trust_key.pem \
  -cert client_trust_cert.pem   \
  -out crl_server_revoked.pem   \
  -config crl.cnf


rm *_csr.pem
