module google.golang.org/grpc/stats/opentelemetry

go 1.21

toolchain go1.22.1

replace google.golang.org/grpc => ../..

require (
	go.opentelemetry.io/otel v1.26.0
	go.opentelemetry.io/otel/metric v1.26.0
	go.opentelemetry.io/otel/sdk/metric v1.24.0
	google.golang.org/grpc v1.63.2
)

require (
	github.com/go-logr/logr v1.4.1 // indirect
	github.com/go-logr/stdr v1.2.2 // indirect
	go.opentelemetry.io/otel/sdk v1.24.0 // indirect
	go.opentelemetry.io/otel/trace v1.26.0 // indirect
	golang.org/x/net v0.25.0 // indirect
	golang.org/x/sys v0.20.0 // indirect
	golang.org/x/text v0.15.0 // indirect
	google.golang.org/genproto/googleapis/rpc v0.0.0-20240509183442-62759503f434 // indirect
	google.golang.org/protobuf v1.34.1 // indirect
)
