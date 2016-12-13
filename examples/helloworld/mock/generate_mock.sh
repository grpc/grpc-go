mockgen google.golang.org/grpc/examples/helloworld/helloworld GreeterClient > mock_helloworld/hw_mock.go
gofmt -w mock_helloworld/hw_mock.go
