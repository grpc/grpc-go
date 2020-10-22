# Credential Reloading From Files

Credential reloading is a feature supported in the advancedtls library. 
A very common way to achieve this is to reload from files.

In this example, users will need to provide a set of locations on the file system holding the credential data.
Once the credential data needs to be updated, users just need to change the credential data in the file system, and gRPC will pick up the changes automatically.

There are several important behaviors to consider:
 1. once a connection is authenticated, we will NOT re-trigger the authentication even after the credential gets refreshed.
 2. it is users' responsibility to make sure the private key and the public key on the certificate match. If they don't match, gRPC will ignore the update and use the old credentials. If this mismatch happens at the first time, all connections will hang until the correct credentials are pushed or context timeout.  

## Try it
In directory `security/advancedtls/examples`:

```
go run server/main.go
```

```
go run client/main.go
```