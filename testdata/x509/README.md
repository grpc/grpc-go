# X.509 Test Credentials

This directory contains X.509 certificates and private keys used in gRPC-Go TLS tests (such as `test/multi_cert_tls_test.go` and other end-to-end integration tests).

## Credentials Overview

### Certificate Authorities (Root CAs)
* **`server_ca_cert.pem` / `server_ca_key.pem`**:
  * **Algorithm**: RSA 4096-bit
  * **Subject**: `CN=test-server_ca, O=gRPC, L=SVL, ST=CA, C=US`
  * **Purpose**: Signs server identity certificates (`server1_cert.pem`, `server2_cert.pem`, `server_ecdsa_cert.pem`).
* **`client_ca_cert.pem` / `client_ca_key.pem`**:
  * **Algorithm**: RSA 4096-bit
  * **Subject**: `CN=test-client_ca, O=gRPC, L=SVL, ST=CA, C=US`
  * **Purpose**: Signs client identity certificates (`client1_cert.pem`, `client2_cert.pem`, `client_ecdsa_cert.pem`, `client_with_spiffe_cert.pem`).

### Server Certificates
* **`server1_cert.pem` / `server1_key.pem`**:
  * **Algorithm**: RSA 4096-bit
  * **Subject**: `CN=test-server1`
  * **SANs**: `DNS:*.test.example.com`, `DNS:*.test.example.com.cn`, `DNS:waterzooi.test.google.be`
  * **Issuer**: `server_ca_cert.pem`
* **`server2_cert.pem` / `server2_key.pem`**:
  * **Algorithm**: RSA 4096-bit
  * **Subject**: `CN=test-server2`
  * **SANs**: `DNS:*.test.example.com`, `DNS:*.test.example.com.cn`, `DNS:waterzooi.test.google.be`
  * **Issuer**: `server_ca_cert.pem`
* **`server_ecdsa_cert.pem` / `server_ecdsa_key.pem`**:
  * **Algorithm**: ECDSA P-256 (`prime256v1`)
  * **Subject**: `CN=test-server-ecdsa`
  * **SANs**: `DNS:*.test.example.com`, `DNS:*.test.example.com.cn`, `DNS:waterzooi.test.google.be`
  * **Issuer**: `server_ca_cert.pem`
  * **Purpose**: Used for multiple-certificate negotiation tests in TLS 1.3 and TLS 1.2 alongside RSA server certificates.

### Client Certificates
* **`client1_cert.pem` / `client1_key.pem`**:
  * **Algorithm**: RSA 4096-bit
  * **Subject**: `CN=test-client1`
  * **SANs**: `DNS:*.test.example.com`, `DNS:*.test.example.com.cn`, `DNS:waterzooi.test.google.be`
  * **Issuer**: `client_ca_cert.pem`
* **`client2_cert.pem` / `client2_key.pem`**:
  * **Algorithm**: RSA 4096-bit
  * **Subject**: `CN=test-client2`
  * **SANs**: `DNS:*.test.example.com`, `DNS:*.test.example.com.cn`, `DNS:waterzooi.test.google.be`
  * **Issuer**: `client_ca_cert.pem`
* **`client_ecdsa_cert.pem` / `client_ecdsa_key.pem`**:
  * **Algorithm**: ECDSA P-256 (`prime256v1`)
  * **Subject**: `CN=test-client-ecdsa`
  * **SANs**: `DNS:*.test.example.com`, `DNS:*.test.example.com.cn`, `DNS:waterzooi.test.google.be`
  * **Issuer**: `client_ca_cert.pem`
  * **Purpose**: Used for mutual TLS (mTLS) tests with ECDSA client authentication.

### SPIFFE & Custom SAN Certificates
* **`spiffe_cert.pem` / `spiffe_key.pem`**:
  * **Algorithm**: RSA 4096-bit (Self-signed)
  * **Subject**: `CN=test-client1`
  * **SAN**: `URI:spiffe://foo.bar.com/client/workload/1`
* **`multiple_uri_cert.pem` / `multiple_uri_key.pem`**:
  * **Algorithm**: RSA 4096-bit (Self-signed)
  * **Subject**: `CN=test-client1`
  * **SANs**: `URI:spiffe://foo.bar.com/client/workload/1`, `URI:https://bar.baz.com/client`
* **`client_with_spiffe_cert.pem` / `client_with_spiffe_key.pem`**:
  * **Algorithm**: RSA 4096-bit
  * **Subject**: `CN=test-client1`
  * **SANs**: `URI:spiffe://foo.bar.com/client/workload/1`, `DNS:*.test.example.com`
  * **Issuer**: `client_ca_cert.pem`

## Certificate Generation

All certificates and keys in this directory are generated using the `create.sh` script:

```bash
./create.sh
```
