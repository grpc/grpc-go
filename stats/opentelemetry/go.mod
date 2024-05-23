module google.golang.org/grpc/stats/opentelemetry

go 1.21

replace google.golang.org/grpc => ../..

require (
	github.com/google/go-cmp v0.6.0
	go.opentelemetry.io/contrib/detectors/gcp v1.26.0
	go.opentelemetry.io/otel v1.26.0
	go.opentelemetry.io/otel/metric v1.26.0
	go.opentelemetry.io/otel/sdk v1.26.0
	go.opentelemetry.io/otel/sdk/metric v1.24.0
	google.golang.org/grpc v1.62.1
	google.golang.org/protobuf v1.33.0
)

require (
	cloud.google.com/go/compute/metadata v0.3.0 // indirect
	github.com/GoogleCloudPlatform/opentelemetry-operations-go/detectors/gcp v1.22.0 // indirect
	github.com/cncf/xds/go v0.0.0-20240318125728-8a4994d93e50 // indirect
	github.com/envoyproxy/go-control-plane v0.12.0 // indirect
	github.com/envoyproxy/protoc-gen-validate v1.0.4 // indirect
	github.com/go-logr/logr v1.4.1 // indirect
	github.com/go-logr/stdr v1.2.2 // indirect
	github.com/golang/protobuf v1.5.4 // indirect
	go.opentelemetry.io/otel/trace v1.26.0 // indirect
	golang.org/x/net v0.22.0 // indirect
	golang.org/x/oauth2 v0.18.0 // indirect
	golang.org/x/sync v0.6.0 // indirect
	golang.org/x/sys v0.19.0 // indirect
	golang.org/x/text v0.14.0 // indirect
	google.golang.org/appengine v1.6.8 // indirect
	google.golang.org/genproto/googleapis/rpc v0.0.0-20240318140521-94a12d6c2237 // indirect
)
