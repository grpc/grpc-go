# CRL Test Data

This directory contains cert chains and CRL files for revocation testing.

To print the chain, use a command like,

```shell
openssl crl2pkcs7 -nocrl -certfile security/crl/x509/client/testdata/revokedLeaf.pem | openssl pkcs7 -print_certs -text -noout
```

The crl file symlinks are generated with `openssl rehash`

## unrevoked.pem

A certificate chain with CRL files and unrevoked certs

*   Subject: C=US, ST=California, L=Mountain View, O=Google LLC, OU=Production,
    OU=campus-sln, CN=Root CA (2021-02-02T07:30:36-08:00)
    *   1.crl

NOTE: 1.crl file is symlinked with 5.crl to simulate two issuers that hash to
the same value to test that loading multiple files works.

*   Subject: C=US, ST=California, L=Mountain View, O=Google LLC, OU=Production,
    OU=campus-sln, CN=node CA (2021-02-02T07:30:36-08:00)
    *   2.crl

## revokedInt.pem

Certificate chain where the intermediate is revoked

*   Subject: C=US, ST=California, L=Mountain View, O=Google LLC, OU=Production,
    OU=campus-sln, CN=Root CA (2021-02-02T07:31:54-08:00)
    *   3.crl
*   Subject: C=US, ST=California, L=Mountain View, O=Google LLC, OU=Production,
    OU=campus-sln, CN=node CA (2021-02-02T07:31:54-08:00)
    *   4.crl

## revokedLeaf.pem

Certificate chain where the leaf is revoked

*   Subject: C=US, ST=California, L=Mountain View, O=Google LLC, OU=Production,
    OU=campus-sln, CN=Root CA (2021-02-02T07:32:57-08:00)
    *   5.crl
*   Subject: C=US, ST=California, L=Mountain View, O=Google LLC, OU=Production,
    OU=campus-sln, CN=node CA (2021-02-02T07:32:57-08:00)
    *   6.crl

## Test Data for testing CRL providers functionality

To generate test data please run provider_create.sh script. All the files have
`provider_` prefix.

We need to generate the following artifacts for testing CRL provider:
* server self signed CA cert
* client self signed CA cert
* server cert signed by client CA
* client cert signed by server CA
* empty crl file
* crl file containing information about revoked server cert
* crl file by 'malicious' CA which contains the same issuer with original CA


All the commands are provided in provider_create.sh script. Please find the
description below.

1. The first two commands generate self signed CAs for client and server:
   - provider_server_trust_key.pem
   - provider_server_trust_cert.pem
   - provider_client_trust_key.pem
   - provider_client_trust_cert.pem

2. Generate client and server certs signed by the CAs above:
   - provider_server_cert.pem
   - provider_client_cert.pem

3. The next 2 commands create 2 files needed for CRL issuing:
   - provider_crlnumber.txt
   - provider_index.txt

4. The next 3 commands generate an empty CRL file and a CRL file containing
revoked server cert:
   - provider_crl_empty.pem
   - provider_crl_server_revoked.pem

5. The final section contains commands to generate CRL file by 'malicious' CA.
Note that we use Subject Key Identifier from previously created
provider_client_trust_cert.pem to generate malicious certs / CRL.
   - provider_malicious_client_trust_key.pem
   - provider_malicious_client_trust_cert.pem
   - provider_malicious_crl_empty.pem
