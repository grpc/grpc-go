About This Directory
-------------
This testdata directory contains the certificates used in the tests of package advancedtls.

How to Generate Test Certificates Using OpenSSL
-------------

Supposing we are going to create a `subject_cert.pem` that is trusted by `ca_cert.pem`, here are the
commands we run: 

1. Generate the private key, `ca_key.pem`, and the cert `ca_cert.pem`, for the CA:

   ```
   $ openssl req -x509 -newkey rsa:4096 -keyout ca_key.pem -out ca_cert.pem -nodes -days $DURATION_DAYS
   ```

2. Generate a CSR `csr.pem` using `subject_key.pem`:

   ```
   $ openssl req -new -key subject_key.pem -out csr.pem
   ```

3. Generate a private key `subject_key.pem` for the subject: 
   
   ```
   $ openssl genrsa -out subject_key.pem 4096
   ```

4. Use `ca_key.pem` and `ca_cert.pem` to sign `csr.pem`, and get a certificate, `subject_cert.pem`, for the subject:
   
   This step requires some additional files and please check out [this answer from StackOverflow](https://stackoverflow.com/a/21340898) for more.

   ```
   $ openssl ca -config openssl-ca.cnf -policy signing_policy -extensions signing_req -out subject_cert.pem -in csr.pem -keyfile ca_key.pem -cert ca_cert.pem
   ```
5. Verify the `subject_cert.pem` is trusted by `ca_cert.pem`:
   

   ```
   $ openssl verify -verbose -CAfile ca_cert.pem  subject_cert.pem

   ```
