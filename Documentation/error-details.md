# Error Details

gRPC servers return a single `error` in response to client requests. The error
value contains a code and a description. In some cases, though, it may be
necessary to add details about the particular error. gRPC includes a
[status.Status][status] type for this purpose.

Upon encountering an error, a server might create a `status.Status` with
[status.New][new-status] using a [codes.Code][code] and a message, and then add
details using [status.WithDetails][with-details]. Once all the necessary details
have been added to `status.Status`, calling [status.Err][status-err] will
convert the `status.Status` back to an `error` type which can then be used as a
return value for the particular gRPC server invocation.

Once the server has returned a detailed `error`, clients may then read those
details by converting the plain `error` type back to a [status.Status][status]
and then using [status.Details][details].

## Example

The [example][example] demonstrates how to add information about rate limits to
the error message using `status.Status`.

To run the example, first start the server:

```
$ go run examples/error_details/server/main.go
```

In a separate sesssion, run the client:

```
$ go run examples/error_details/client/main.go
```

On the first run of the client, all is well:

```
2018/03/12 19:39:33 Greeting: Hello world
```

Upon running the client a second time, the client exceeds the rate limit and
receives an error with details:

```
2018/03/19 16:42:01 Quota failure: violations:<subject:"name:world" description:"Limit one greeting per person" >
exit status 1
```

[status]:       https://godoc.org/google.golang.org/grpc/status#Status
[new-status]:   https://godoc.org/google.golang.org/grpc/status#New
[code]:         https://godoc.org/google.golang.org/grpc/codes#Code
[with-details]: https://godoc.org/google.golang.org/grpc/status#Status.WithDetails
[details]:      https://godoc.org/google.golang.org/grpc/status#Status.Details
[status-err]:   https://godoc.org/google.golang.org/grpc/status#Status.Err
[example]:      https://github.com/grpc/grpc-go/blob/master/examples/error_details
