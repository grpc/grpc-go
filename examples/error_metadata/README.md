# Setting and Reading Error Details

gRPC servers return a single error value in response to client requests.  The
error value contains an error code and a description. In some cases more details
regarding the error are required in order to take action. The recommended way to
specify error details is by attaching metadata to the returned errors using
`status.WithDetails`. Clients may then read those errors by converting an error
to a `status.Status` and using `status.Details`.

## Running the example

Run the server:

```
$ go run server/main.go
```

In a separate sesssion, run the client:

```
$ go run client/main.go
```

Output:

```
2018/03/12 19:39:33 Greeting: Hello world
```

Run the client again:

```
$ go run client/main.go
```

Output:

```
2018/03/12 19:39:35 ResourceInfo details: violations:<subject:"name:world" description:"Limit one greeting per person" >
exit status 1
```
