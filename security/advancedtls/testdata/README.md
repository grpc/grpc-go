# AdvancedTLS Test Credentials

This testdata directory contains X.509 certificates and private keys used in the tests for the `advancedtls` package.

## Credentials Overview

### Trust Roots / Certificate Authorities (CAs)
* **`client_trust_cert_1.pem` / `client_trust_key_1.pem`**:
  * **Algorithm**: RSA 4096-bit (Self-signed Root CA)
  * **Subject**: `C=US, ST=CA, L=SVL, O=Internet Widgits Pty Ltd`
  * **Purpose**: Used on client side to verify server identity. Signs `server_cert_1.pem`, `server_cert_localhost_1.pem`, `server_ecdsa_cert_1.pem`, and `server_ecdsa_cert_localhost_1.pem`.
* **`client_trust_cert_2.pem` / `client_trust_key_2.pem`**:
  * **Algorithm**: RSA 4096-bit (Self-signed Root CA)
  * **Subject**: `C=US, ST=CA, O=Internet Widgits Pty Ltd, CN=foo.bar.client2.trust.com`
  * **Purpose**: Used on client side to verify server identity. Signs `server_cert_2.pem`.
* **`server_trust_cert_1.pem` / `server_trust_key_1.pem`**:
  * **Algorithm**: RSA 4096-bit (Self-signed Root CA)
  * **Subject**: `C=US, ST=VA, O=Internet Widgits Pty Ltd, CN=foo.bar.hoo.ca.com`
  * **Purpose**: Used on server side to verify client identity in mTLS. Signs `client_cert_1.pem`, `client_ecdsa_cert_1.pem`, `client_ecdsa_cert_localhost_1.pem`, and `another_client_cert_1.pem`.
* **`server_trust_cert_2.pem` / `server_trust_key_2.pem`**:
  * **Algorithm**: RSA 4096-bit (Self-signed Root CA)
  * **Subject**: `C=US, ST=CA, O=Internet Widgits Pty Ltd, CN=foo.bar.server2.trust.com`
  * **Purpose**: Used on server side to verify client identity in mTLS. Signs `client_cert_2.pem`.

### Server Identity Certificates
* **`server_cert_1.pem` / `server_key_1.pem`**:
  * **Algorithm**: RSA 4096-bit
  * **Subject**: `CN=foo.bar.com`
  * **Issuer**: `client_trust_cert_1.pem`
* **`server_cert_2.pem` / `server_key_2.pem`**:
  * **Algorithm**: RSA 4096-bit
  * **Subject**: `CN=foo.bar.server2.com`
  * **Issuer**: `client_trust_cert_2.pem`
* **`server_cert_3.pem` / `server_key_3.pem`**:
  * **Algorithm**: RSA 2048-bit (Self-signed)
  * **Subject**: `CN=foo.bar.server3.com`
  * **SANs**: `DNS:google.com`, `DNS:apple.com`, `DNS:amazon.com`
* **`server_cert_localhost_1.pem` / `server_key_localhost_1.pem`**:
  * **Algorithm**: RSA 4096-bit
  * **Subject**: `CN=localhost`
  * **SAN**: `DNS:localhost`
  * **Issuer**: `client_trust_cert_1.pem`
* **`server_ecdsa_cert_1.pem` / `server_ecdsa_key_1.pem`**:
  * **Algorithm**: ECDSA P-256 (`prime256v1`)
  * **Subject**: `CN=foo.bar.com`
  * **Issuer**: `client_trust_cert_1.pem`
  * **Purpose**: Used for TLS 1.3 / TLS 1.2 multi-certificate algorithm negotiation and fallback tests.
* **`server_ecdsa_cert_localhost_1.pem` / `server_ecdsa_key_localhost_1.pem`**:
  * **Algorithm**: ECDSA P-256 (`prime256v1`)
  * **Subject**: `CN=localhost`
  * **SAN**: `DNS:localhost`
  * **Issuer**: `client_trust_cert_1.pem`
  * **Purpose**: Used for dual-certificate TLS 1.3 / TLS 1.2 SNI and negotiation integration tests connecting to localhost.

### Client Identity Certificates
* **`client_cert_1.pem` / `client_key_1.pem`**:
  * **Algorithm**: RSA 4096-bit
  * **Subject**: `CN=foo.bar.hoo.com`
  * **Issuer**: `server_trust_cert_1.pem`
* **`client_cert_2.pem` / `client_key_2.pem`**:
  * **Algorithm**: RSA 4096-bit
  * **Subject**: `CN=foo.bar.client2.com`
  * **Issuer**: `server_trust_cert_2.pem`
* **`another_client_cert_1.pem` / `another_client_key_1.pem`**:
  * **Algorithm**: RSA 4096-bit
  * **Subject**: `CN=foo.bar.another.client.com`
  * **Issuer**: `server_trust_cert_1.pem`
* **`client_ecdsa_cert_1.pem` / `client_ecdsa_key_1.pem`**:
  * **Algorithm**: ECDSA P-256 (`prime256v1`)
  * **Subject**: `CN=foo.bar.hoo.com`
  * **Issuer**: `server_trust_cert_1.pem`
  * **Purpose**: Used for mutual TLS (mTLS) with ECDSA client authentication.
* **`client_ecdsa_cert_localhost_1.pem` / `client_ecdsa_key_localhost_1.pem`**:
  * **Algorithm**: ECDSA P-256 (`prime256v1`)
  * **Subject**: `CN=localhost`
  * **SAN**: `DNS:localhost`
  * **Issuer**: `server_trust_cert_1.pem`
  * **Purpose**: Used for mutual TLS (mTLS) with ECDSA client authentication connecting to localhost.

### Certificate Revocation List (CRL) Testdata
* **`crl/`**:
  * Contains CA certificates, client/server certificates, and CRL files (both empty and revoked) used for certificate revocation testing in `advancedtls`.

## Certificate Generation

All certificates and keys in this directory can be generated using the `create.sh` script:

```bash
./create.sh
```

To generate CRL test certificates, run `provider_create.sh` in the `crl/` subdirectory:

```bash
cd crl && ./provider_create.sh
```
