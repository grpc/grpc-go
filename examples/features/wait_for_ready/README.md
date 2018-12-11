# Wait for ready example

This example shows how to enable wait for ready (or more acurately disable fail fast) in RPC calls.

This code starts the server with a 2 seconds delay. If `failfast` is not disabled, then the RPC immediately fails with `Unavailable` code (case 1). If `failfast` is disabled, then RPC waits for the server. If context dies before the server is available, then it fails with `DeadlineExceeded` (case 3). Otherwise it succeeds (case 2).

## Run the example

```
go run main.go
```
