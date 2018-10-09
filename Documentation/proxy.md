# Proxy

HTTP CONNECT proxy is support by default in gRPC. The proxy address can be
specified by environment variables HTTP_PROXY, HTTPS_PROXY and NO_PROXY (or the
lowercase versions thereof).

## Custom proxy

Currently, the proxy is implemented as a custom dialer. It does one more
handshake (a CONNECT handshake in the case of HTTP CONNECT proxy) on the
connection before giving it to gRPC.

If the default proxy doesn't work for you, replace the default dialer with your
custom proxy dialer. This can be done using
[`WithDialer`](https://godoc.org/google.golang.org/grpc#WithDialer).