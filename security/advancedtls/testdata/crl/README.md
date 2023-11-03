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

To generate test data please follow the steps below or run provider_create.sh 
script. All the files have `provider_` prefix.

We need to generate the following artifacts for testing CRL provider:
* server self signed CA cert
* client self signed CA cert
* server cert signed by client CA
* client cert signed by server CA
* empty crl file
* crl file containing information about revoked server cert

Please find the related commands below.

* Generate self signed CAs
```
$ openssl req -x509 -newkey rsa:4096 -keyout provider_server_trust_key.pem -out provider_server_trust_cert.pem -days 365 -subj "/C=US/ST=VA/O=Internet Widgits Pty Ltd/CN=foo.bar.hoo.ca.com" -nodes
$ openssl req -x509 -newkey rsa:4096 -keyout provider_client_trust_key.pem -out provider_client_trust_cert.pem -days 365 -subj "/C=US/ST=CA/L=SVL/O=Internet Widgits Pty Ltd" -nodes
```

* Generate client and server certs signed by CAs
```
$ openssl req -newkey rsa:4096 -keyout provider_server_cert.key -out provider_new_cert.csr -nodes -subj "/C=US/ST=CA/L=DUMMYCITY/O=Internet Widgits Pty Ltd/CN=foo.bar.com" -sha256
$ openssl x509 -req -in provider_new_cert.csr -out provider_server_cert.pem -CA provider_client_trust_cert.pem -CAkey provider_client_trust_key.pem -CAcreateserial -days 3650 -sha256 -extfile provider_extensions.conf

$ openssl req -newkey rsa:4096 -keyout provider_client_cert.key -out provider_new_cert.csr -nodes -subj "/C=US/ST=CA/O=Internet Widgits Pty Ltd/CN=foo.bar.hoo.com" -sha256
$ openssl x509 -req -in provider_new_cert.csr -out provider_client_cert.pem -CA provider_server_trust_cert.pem -CAkey provider_server_trust_key.pem -CAcreateserial -days 3650 -sha256 -extfile provider_extensions.conf
```

Here is the content of `provider_extensions.conf` -
```
subjectKeyIdentifier = hash
authorityKeyIdentifier = keyid,issuer
basicConstraints = CA:FALSE
keyUsage = digitalSignature, keyEncipherment
```

* Generate CRLs
  For CRL generation we need 2 more files called `index.txt` and `crlnumber.txt`:
```
$ echo "1000" > provider_crlnumber.txt
$ touch provider_index.txt
```
Also we need another config `provider_crl.cnf` -
```
[ ca ]
default_ca = my_ca

[ my_ca ]
crl = crl.pem
default_md = sha256
database = provider_index.txt
crlnumber = provider_crlnumber.txt
default_crl_days = 30
default_crl_hours = 1
crl_extensions = crl_ext

[crl_ext]
# Authority Key Identifier extension
authorityKeyIdentifier=keyid:always,issuer:always
```

The commands to generate empty CRL file and CRL file containing revoked server
cert are below.
```
$ openssl ca -gencrl -keyfile provider_client_trust_key.pem -cert provider_client_trust_cert.pem -out provider_crl_empty.pem -config provider_crl.cnf
$ openssl ca -revoke provider_server_cert.pem -keyfile provider_client_trust_key.pem -cert provider_client_trust_cert.pem -config provider_crl.cnf
$ openssl ca -gencrl -keyfile provider_client_trust_key.pem -cert provider_client_trust_cert.pem -out provider_crl_server_revoked.pem -config provider_crl.cnf
```