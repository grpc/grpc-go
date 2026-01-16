# Proxy

HTTP CONNECT proxies are supported by default in gRPC. The proxy address can be
specified by the environment variables `HTTPS_PROXY` and `NO_PROXY`.  (Note that
these environment variables are case insensitive.)

**NOTE**: Talking to CONNECT proxies using https is not supported. gRPC performs
a plaintext CONNECT handshake to establish a tunnel and does not support the
additional encryption required to secure the initial connection to the proxy
itself. We have an open [feature
request](https://github.com/grpc/grpc/issues/35372) for it and might support it
in future.

Not using https to connect to HTTP CONNECT proxy does not compromise security.
The gRPC traffic is encrypted end-to-end between the client and the destination
server. The HTTP CONNECT proxy only sees the destination address and cannot
intercept the encrypted gRPC data.

## Custom proxy

Currently, proxy support is implemented in the default dialer. It does one more
handshake (a CONNECT handshake in the case of HTTP CONNECT proxy) on the
connection before giving it to gRPC.

If the default proxy doesn't work for you, replace the default dialer with your
custom proxy dialer. This can be done using
[`WithContextDialer`](https://pkg.go.dev/google.golang.org/grpc#WithContextDialer).
