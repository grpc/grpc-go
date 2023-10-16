About This Directory
-------------
The directory test data (certificates and CRLs) used for testing CRL providers 
functionality.

How to Generate Test Data Using OpenSSL
-------------

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
$ openssl req -x509 -newkey rsa:4096 -keyout server_trust_key.pem -out server_trust_cert.pem -days 365 -subj "/C=US/ST=VA/O=Internet Widgits Pty Ltd/CN=foo.bar.hoo.ca.com" -nodes
$ openssl req -x509 -newkey rsa:4096 -keyout client_trust_key.pem -out client_trust_cert.pem -days 365 -subj "/C=US/ST=CA/L=SVL/O=Internet Widgits Pty Ltd" -nodes
```

* Generate client and server certs signed by CAs
```
$ openssl req -newkey rsa:4096 -keyout server_cert.key -out new_cert.csr -nodes -subj "/C=US/ST=CA/L=DUMMYCITY/O=Internet Widgits Pty Ltd/CN=foo.bar.com" -sha256
$ openssl x509 -req -in new_cert.csr -out server_cert.pem -CA client_trust_cert.pem -CAkey client_trust_key.pem -CAcreateserial -days 3650 -sha256 -extfile extensions.conf

$ openssl req -newkey rsa:4096 -keyout client_cert.key -out new_cert.csr -nodes -subj "/C=US/ST=CA/O=Internet Widgits Pty Ltd/CN=foo.bar.hoo.com" -sha256
$ openssl x509 -req -in new_cert.csr -out client_cert.pem -CA server_trust_cert.pem -CAkey server_trust_key.pem -CAcreateserial -days 3650 -sha256 -extfile extensions.conf
```

Here is the content of `extensions.conf` - 
```
subjectKeyIdentifier = hash
authorityKeyIdentifier = keyid,issuer
basicConstraints = CA:FALSE
keyUsage = digitalSignature, keyEncipherment
```

* Generate CRLs
For CRL generation we need 2 more files called `index.txt` and `crlnumber.txt`:
```
$ echo "1000" > crlnumber.txt
$ touch index.txt
```
Also we need anothe config `crl.cnf` - 
```
[ ca ]
default_ca = my_ca

[ my_ca ]
crl = crl.pem
default_md = sha256
database = index.txt
crlnumber = crlnumber.txt
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
$ openssl ca -gencrl -keyfile client_trust_key.pem -cert client_trust_cert.pem -out crl_empty.pem -config crl.cnf
$ openssl ca -revoke server_cert.pem -keyfile client_trust_key.pem -cert client_trust_cert.pem -config crl.cnf
$ openssl ca -gencrl -keyfile client_trust_key.pem -cert client_trust_cert.pem -out crl_server_revoked.pem -config crl.cnf
```