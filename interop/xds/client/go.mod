module google.golang.org/interop/test/client

go 1.21

replace google.golang.org/grpc => ../../..

replace google.golang.org/grpc/stats/opentelemetry => ../../../stats/opentelemetry

require (
	github.com/prometheus/client_golang v1.19.1
	go.opentelemetry.io/otel/exporters/prometheus v0.49.0
	go.opentelemetry.io/otel/sdk/metric v1.27.0
	google.golang.org/grpc v1.64.0
	google.golang.org/grpc/stats/opentelemetry v0.0.0-20240523232201-f7d3d3eecbee
)

require (
	cel.dev/expr v0.15.0 // indirect
	cloud.google.com/go/compute/metadata v0.3.0 // indirect
	github.com/GoogleCloudPlatform/opentelemetry-operations-go/detectors/gcp v1.23.0 // indirect
	github.com/beorn7/perks v1.0.1 // indirect
	github.com/census-instrumentation/opencensus-proto v0.4.1 // indirect
	github.com/cespare/xxhash/v2 v2.3.0 // indirect
	github.com/cncf/xds/go v0.0.0-20240423153145-555b57ec207b // indirect
	github.com/envoyproxy/go-control-plane v0.12.0 // indirect
	github.com/envoyproxy/protoc-gen-validate v1.0.4 // indirect
	github.com/go-logr/logr v1.4.2 // indirect
	github.com/go-logr/stdr v1.2.2 // indirect
	github.com/golang/protobuf v1.5.4 // indirect
	github.com/prometheus/client_model v0.6.1 // indirect
	github.com/prometheus/common v0.53.0 // indirect
	github.com/prometheus/procfs v0.15.0 // indirect
	go.opentelemetry.io/contrib/detectors/gcp v1.27.0 // indirect
	go.opentelemetry.io/otel v1.27.0 // indirect
	go.opentelemetry.io/otel/metric v1.27.0 // indirect
	go.opentelemetry.io/otel/sdk v1.27.0 // indirect
	go.opentelemetry.io/otel/trace v1.27.0 // indirect
	golang.org/x/net v0.25.0 // indirect
	golang.org/x/oauth2 v0.20.0 // indirect
	golang.org/x/sync v0.7.0 // indirect
	golang.org/x/sys v0.20.0 // indirect
	golang.org/x/text v0.15.0 // indirect
	google.golang.org/genproto/googleapis/api v0.0.0-20240528155852-a33235495d66 // indirect
	google.golang.org/genproto/googleapis/rpc v0.0.0-20240528155852-a33235495d66 // indirect
	google.golang.org/protobuf v1.34.1 // indirect
)
