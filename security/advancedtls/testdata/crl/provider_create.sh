#!/bin/bash

# The script contains a sequence of commands described in README.md
openssl req -x509                                                      \
  -newkey rsa:4096                                                     \
  -keyout provider_server_trust_key.pem                                \
  -out provider_server_trust_cert.pem                                  \
  -days 365                                                            \
  -subj "/C=US/ST=VA/O=Internet Widgits Pty Ltd/CN=foo.bar.hoo.ca.com" \
  -nodes

openssl req -x509                                      \
  -newkey rsa:4096                                     \
  -keyout provider_client_trust_key.pem                \
  -out provider_client_trust_cert.pem                  \
  -days 365                                            \
  -subj "/C=US/ST=CA/L=SVL/O=Internet Widgits Pty Ltd" \
  -nodes

openssl req -newkey rsa:4096                                                \
  -keyout provider_server_cert.key                                          \
  -out provider_new_cert.csr                                                \
  -nodes                                                                    \
  -subj "/C=US/ST=CA/L=DUMMYCITY/O=Internet Widgits Pty Ltd/CN=foo.bar.com" \
  -sha256

openssl x509 -req                      \
  -in provider_new_cert.csr            \
  -out provider_server_cert.pem        \
  -CA provider_client_trust_cert.pem   \
  -CAkey provider_client_trust_key.pem \
  -CAcreateserial                      \
  -days 3650                           \
  -sha256                              \
  -extfile provider_extensions.conf

openssl req -newkey rsa:4096                                        \
  -keyout provider_client_cert.key                                  \
  -out provider_new_cert.csr                                        \
  -nodes                                                            \
  -subj "/C=US/ST=CA/O=Internet Widgits Pty Ltd/CN=foo.bar.hoo.com" \
  -sha256

openssl x509 -req                      \
  -in provider_new_cert.csr            \
  -out provider_client_cert.pem        \
  -CA provider_server_trust_cert.pem   \
  -CAkey provider_server_trust_key.pem \
  -CAcreateserial                      \
  -days 3650                           \
  -sha256                              \
  -extfile provider_extensions.conf

echo "1000" > provider_crlnumber.txt

touch provider_index.txt

openssl ca -gencrl                       \
  -keyfile provider_client_trust_key.pem \
  -cert provider_client_trust_cert.pem   \
  -out provider_crl_empty.pem            \
  -config provider_crl.cnf

openssl ca -revoke provider_server_cert.pem \
  -keyfile provider_client_trust_key.pem    \
  -cert provider_client_trust_cert.pem      \
  -config provider_crl.cnf

openssl ca -gencrl                       \
  -keyfile provider_client_trust_key.pem \
  -cert provider_client_trust_cert.pem   \
  -out provider_crl_server_revoked.pem   \
  -config provider_crl.cnf


rm *.csr
