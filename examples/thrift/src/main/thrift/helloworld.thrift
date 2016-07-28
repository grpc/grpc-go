namespace java io.grpc.examples.thrift.helloworld

struct HelloRequest {
	1:string name
}

struct HelloResponse {
	1:string message
}

service Greeter {
	HelloResponse sayHello(1:HelloRequest request);
}
