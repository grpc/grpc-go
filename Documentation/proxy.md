# Proxy

HTTP CONNECT proxies are supported by default in gRPC. The proxy address can be
specified by the environment variables `HTTPS_PROXY` and `NO_PROXY`.  Before Go
1.16, if the `HTTPS_PROXY` environment variable is unset, `HTTP_PROXY` will be
used instead.  (Note that these environment variables are case insensitive.)

## Custom proxy

Currently, proxy support is implemented in the default dialer. It does one more
handshake (a CONNECT handshake in the case of HTTP CONNECT proxy) on the
connection before giving it to gRPC.

If the default proxy doesn't work for you, replace the default dialer with your
custom proxy dialer. This can be done using
[`WithDialer`](https://godoc.org/google.golang.org/grpc#WithDialer).
