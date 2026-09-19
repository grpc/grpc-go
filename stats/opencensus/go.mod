module google.golang.org/grpc/stats/opencensus

go 1.25.0

require (
	github.com/google/go-cmp v0.7.0
	go.opencensus.io v0.24.0
	google.golang.org/grpc v1.83.0
	google.golang.org/grpc/interop v0.0.0
)

require (
	github.com/golang/groupcache v0.0.0-20241129210726-2c02b8208cf8 // indirect
	golang.org/x/net v0.58.0 // indirect
	golang.org/x/sys v0.47.0 // indirect
	golang.org/x/text v0.41.0 // indirect
	google.golang.org/genproto/googleapis/rpc v0.0.0-20260817212433-ac3dfec99bb1 // indirect
	google.golang.org/protobuf v1.36.12 // indirect
)

replace google.golang.org/grpc => ../..

replace google.golang.org/grpc/interop => ../../interop
