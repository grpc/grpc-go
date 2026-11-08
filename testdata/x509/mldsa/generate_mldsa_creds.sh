#!/bin/bash
# Copyright 2026 gRPC authors.
#
# Licensed under the Apache License, Version 2.0 (the "License");
# you may not use this file except in compliance with the License.
# You may obtain a copy of the License at
#
#     http://www.apache.org/licenses/LICENSE-2.0
#
# Unless required by applicable law or agreed to in writing, software
# distributed under the License is distributed on an "AS IS" BASIS,
# WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
# See the License for the specific language governing permissions and
# limitations under the License.

# This script generates the following ML-DSA test credentials (using the
# RFC 9881 32-byte seed-only private key representation required by Go's
# crypto/x509 and BoringSSL):
# ca_mldsa44.key
# ca_mldsa44.pem
# client_mldsa44.key
# client_mldsa44.pem
# server_mldsa44.key
# server_mldsa44.pem
# ca_mldsa65.key
# ca_mldsa65.pem
# client_mldsa65.key
# client_mldsa65.pem
# server_mldsa65.key
# server_mldsa65.pem
# ca_mldsa87.key
# ca_mldsa87.pem
# client_mldsa87.key
# client_mldsa87.pem
# server_mldsa87.key
# server_mldsa87.pem
# client_chain_mldsa87.key
# client_chain_mldsa87.pem
# server_chain_mldsa87.key
# server_chain_mldsa87.pem

set -e

cd "$(dirname "$0")"

OPENSSL_CNF="$(mktemp)"
trap 'rm -f "${OPENSSL_CNF}" ./*.csr ./*.srl' EXIT

cat > "${OPENSSL_CNF}" <<'EOF'
[req]
distinguished_name = req_distinguished_name
attributes = req_attributes

[req_distinguished_name]

[req_attributes]

[test_ca]
basicConstraints        = critical,CA:TRUE
subjectKeyIdentifier    = hash
authorityKeyIdentifier  = keyid:always,issuer:always
keyUsage                = critical,keyCertSign,cRLSign

[test_server]
basicConstraints        = critical,CA:FALSE
subjectKeyIdentifier    = hash
keyUsage                = critical,digitalSignature
extendedKeyUsage        = serverAuth
subjectAltName          = @server_alt_names

[server_alt_names]
DNS.1 = *.test.example.com
DNS.2 = *.test.google.fr
DNS.3 = waterzooi.test.google.be
DNS.4 = *.test.youtube.com
DNS.5 = localhost
IP.1  = 127.0.0.1

[test_client]
basicConstraints        = critical,CA:FALSE
subjectKeyIdentifier    = hash
keyUsage                = critical,digitalSignature
extendedKeyUsage        = critical,clientAuth
EOF

generate_mldsa_set() {
  local alg="$1"
  local suffix="$2"

  # 1. Generate self-signed Root CA key and cert (seed-only format)
  openssl genpkey -algorithm "${alg}" \
    -provparam ml-dsa.output_formats=seed-only \
    -out "ca_${suffix}.key"
  openssl req -x509 -new -key "ca_${suffix}.key" \
    -out "ca_${suffix}.pem" -days 3650 \
    -config "${OPENSSL_CNF}" -extensions test_ca \
    -subj "/C=US/ST=CA/L=SVL/O=gRPC/CN=test-ca-${suffix}/"

  # 2. Generate client key and cert signed by the ML-DSA Root CA
  openssl genpkey -algorithm "${alg}" \
    -provparam ml-dsa.output_formats=seed-only \
    -out "client_${suffix}.key"
  openssl req -new -key "client_${suffix}.key" \
    -out "client_${suffix}.csr" \
    -config "${OPENSSL_CNF}" -reqexts test_client \
    -subj "/C=US/ST=CA/L=SVL/O=gRPC/CN=test-client-${suffix}/"
  openssl x509 -req -in "client_${suffix}.csr" \
    -CA "ca_${suffix}.pem" -CAkey "ca_${suffix}.key" -CAcreateserial \
    -extfile "${OPENSSL_CNF}" -extensions test_client \
    -out "client_${suffix}.pem" -days 3650
  openssl verify -verbose -CAfile "ca_${suffix}.pem" "client_${suffix}.pem"

  # 3. Generate server key and cert signed by the ML-DSA Root CA
  openssl genpkey -algorithm "${alg}" \
    -provparam ml-dsa.output_formats=seed-only \
    -out "server_${suffix}.key"
  openssl req -new -key "server_${suffix}.key" \
    -out "server_${suffix}.csr" \
    -config "${OPENSSL_CNF}" -reqexts test_server \
    -subj "/C=US/ST=CA/L=SVL/O=gRPC/CN=test-server-${suffix}/"
  openssl x509 -req -in "server_${suffix}.csr" \
    -CA "ca_${suffix}.pem" -CAkey "ca_${suffix}.key" -CAcreateserial \
    -extfile "${OPENSSL_CNF}" -extensions test_server \
    -out "server_${suffix}.pem" -days 3650
  openssl verify -verbose -CAfile "ca_${suffix}.pem" "server_${suffix}.pem"
}

generate_mldsa_chain_set() {
  local alg="$1"
  local suffix="$2"

  # 1. Generate Intermediate CA key and cert signed by the ML-DSA Root CA
  openssl genpkey -algorithm "${alg}" \
    -provparam ml-dsa.output_formats=seed-only \
    -out "intermediate_ca_${suffix}.key"
  openssl req -new -key "intermediate_ca_${suffix}.key" \
    -out "intermediate_ca_${suffix}.csr" \
    -config "${OPENSSL_CNF}" \
    -subj "/C=US/ST=CA/L=SVL/O=gRPC/CN=test-intermediate-ca-${suffix}/"
  openssl x509 -req -in "intermediate_ca_${suffix}.csr" \
    -CA "ca_${suffix}.pem" -CAkey "ca_${suffix}.key" -CAcreateserial \
    -extfile "${OPENSSL_CNF}" -extensions test_ca \
    -out "intermediate_ca_${suffix}.pem" -days 3650

  # 2. Generate client key and leaf cert signed by the Intermediate CA, bundled
  # with the Intermediate CA into a 2-cert chain.
  openssl genpkey -algorithm "${alg}" \
    -provparam ml-dsa.output_formats=seed-only \
    -out "client_chain_${suffix}.key"
  openssl req -new -key "client_chain_${suffix}.key" \
    -out "client_chain_${suffix}.csr" \
    -config "${OPENSSL_CNF}" -reqexts test_client \
    -subj "/C=US/ST=CA/L=SVL/O=gRPC/CN=test-client-chain-${suffix}/"
  openssl x509 -req -in "client_chain_${suffix}.csr" \
    -CA "intermediate_ca_${suffix}.pem" \
    -CAkey "intermediate_ca_${suffix}.key" -CAcreateserial \
    -extfile "${OPENSSL_CNF}" -extensions test_client \
    -out "client_leaf_${suffix}.pem" -days 3650
  cat "client_leaf_${suffix}.pem" "intermediate_ca_${suffix}.pem" \
    > "client_chain_${suffix}.pem"
  openssl verify -verbose -CAfile "ca_${suffix}.pem" \
    -untrusted "intermediate_ca_${suffix}.pem" "client_leaf_${suffix}.pem"

  # 3. Generate server key and leaf cert signed by the Intermediate CA, bundled
  # with the Intermediate CA into a 2-cert chain.
  openssl genpkey -algorithm "${alg}" \
    -provparam ml-dsa.output_formats=seed-only \
    -out "server_chain_${suffix}.key"
  openssl req -new -key "server_chain_${suffix}.key" \
    -out "server_chain_${suffix}.csr" \
    -config "${OPENSSL_CNF}" -reqexts test_server \
    -subj "/C=US/ST=CA/L=SVL/O=gRPC/CN=test-server-chain-${suffix}/"
  openssl x509 -req -in "server_chain_${suffix}.csr" \
    -CA "intermediate_ca_${suffix}.pem" \
    -CAkey "intermediate_ca_${suffix}.key" -CAcreateserial \
    -extfile "${OPENSSL_CNF}" -extensions test_server \
    -out "server_leaf_${suffix}.pem" -days 3650
  cat "server_leaf_${suffix}.pem" "intermediate_ca_${suffix}.pem" \
    > "server_chain_${suffix}.pem"
  openssl verify -verbose -CAfile "ca_${suffix}.pem" \
    -untrusted "intermediate_ca_${suffix}.pem" "server_leaf_${suffix}.pem"

  rm -f "intermediate_ca_${suffix}.key" "intermediate_ca_${suffix}.pem" \
    "client_leaf_${suffix}.pem" "server_leaf_${suffix}.pem"
}

generate_mldsa_set "ML-DSA-44" "mldsa44"
generate_mldsa_set "ML-DSA-65" "mldsa65"
generate_mldsa_set "ML-DSA-87" "mldsa87"
generate_mldsa_chain_set "ML-DSA-87" "mldsa87"

chmod 644 ./*.key ./*.pem
rm -f ./*.csr ./*.srl
