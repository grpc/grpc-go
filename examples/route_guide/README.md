# Description
The route guide server and client demonstrates how to use grpc go libraries to
perform unary, client streaming, server streaming and full duplex RPCs.

See the definition of the route guide service in route_guide.proto.

# Run the sample code
To compile and run the server, assuming you are in the root of the route_guide
folder, i.e., .../examples/route_guide/, simply:

`go run server/server.go`

Likewise, to run the client:

`go run client/client.go`

# Optional command line flags
The server and client both take optional command line flags. For example, the
client and server run without TLS by default. To enable TSL:

`go run server/server.go -use_tls=true`
`go run client/client.go -use_tls=true`
