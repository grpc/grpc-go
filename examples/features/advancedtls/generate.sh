#!/bin/bash
if [ -d "creds" ]; then
  echo "creds directory already exists. Remove it and re-run this script."
  exit 1
fi
mkdir creds
pushd creds
touch index.txt
echo "01" > serial.txt
cp "../localhost-openssl.cnf" .
cp "../openssl-ca.cnf" .

# Create the CA private key and certificate
openssl req -x509 -newkey rsa:4096 -keyout ca_key.pem -out ca_cert.pem -nodes -days 3650 -subj "/C=US/ST=Georgia/L=Atlanta/O=Test CA/OU=Test CA Organzation/CN=Test CA Organization/emailAddress=test@example.com"

#################### Server cert

# Generate Server private key
openssl genrsa -out server_key.pem 4096

# Generate Server Certificate Signing Request (CSR)
openssl req -config "localhost-openssl.cnf" -new -key server_key.pem -out server_csr.pem -subj "/C=US/ST=Georgia/L=Atlanta/O=Test Server/OU=Test Server Organzation/CN=Test Server Organization/emailAddress=testserver@example.com"

# Use the CA to sign the Server CSR
openssl ca -config "openssl-ca.cnf" -policy signing_policy -extensions signing_req -out server_cert.pem -in server_csr.pem -keyfile ca_key.pem -cert ca_cert.pem -batch

# Verify the server cert works
openssl verify -verbose -CAfile ca_cert.pem server_cert.pem

## Generate another server cert to be revoked
openssl req -config "localhost-openssl.cnf" -new -key server_key.pem -out server_csr_revoked.pem -subj "/C=US/ST=Georgia/L=Atlanta/O=Test server/OU=Test server Organzation/CN=Test server Organization/emailAddress=testserver@example.com"

# Use the CA to sign the server CSR
openssl ca -config "openssl-ca.cnf" -policy signing_policy -extensions signing_req -out server_cert_revoked.pem -in server_csr_revoked.pem -keyfile ca_key.pem -cert ca_cert.pem -batch

# Verify the server cert works
openssl verify -verbose -CAfile ca_cert.pem server_cert_revoked.pem

# Revoke the cert
openssl ca -config "openssl-ca.cnf" -revoke server_cert_revoked.pem

# Generate the CRL
openssl ca -config "openssl-ca.cnf" -gencrl -out server_revoked.crl

# Make sure the cert is actually revoked
openssl verify -verbose -CAfile ca_cert.pem -CRLfile server_revoked.crl -crl_check_all server_cert_revoked.pem

#################### Client cert
# Generate client private key
openssl genrsa -out client_key.pem 4096

# Generate client Certificate Signing Request (CSR)
openssl req -config "localhost-openssl.cnf" -new -key client_key.pem -out client_csr.pem -subj "/C=US/ST=Georgia/L=Atlanta/O=Test client/OU=Test client Organzation/CN=Test client Organization/emailAddress=testclient@example.com"

# Use the CA to sign the client CSR
openssl ca -config "openssl-ca.cnf" -policy signing_policy -extensions signing_req -out client_cert.pem -in client_csr.pem -keyfile ca_key.pem -cert ca_cert.pem -batch

# Verify the client cert works
openssl verify -verbose -CAfile ca_cert.pem client_cert.pem

## Generate another client cert to be revoked
openssl req -config "localhost-openssl.cnf" -new -key client_key.pem -out client_csr_revoked.pem -subj "/C=US/ST=Georgia/L=Atlanta/O=Test client/OU=Test client Organzation/CN=Test client Organization/emailAddress=testclient@example.com"

# Use the CA to sign the client CSR
openssl ca -config "openssl-ca.cnf" -policy signing_policy -extensions signing_req -out client_cert_revoked.pem -in client_csr_revoked.pem -keyfile ca_key.pem -cert ca_cert.pem -batch

# Verify the client cert works
openssl verify -verbose -CAfile ca_cert.pem client_cert_revoked.pem

# Revoke the cert
openssl ca -config "openssl-ca.cnf" -revoke client_cert_revoked.pem

# Generate the CRL
openssl ca -config "openssl-ca.cnf" -gencrl -out client_revoked.crl

# Make sure the cert is actually revoked
openssl verify -verbose -CAfile ca_cert.pem -CRLfile client_revoked.crl -crl_check_all client_cert_revoked.pem

mkdir crl
mv client_revoked.crl crl/

rm 01.pem
rm 02.pem
rm 03.pem
rm 04.pem
rm *csr*
rm *.txt*



popd
