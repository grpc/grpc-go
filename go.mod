module google.golang.org/grpc

go 1.24.0

require (
	github.com/cespare/xxhash/v2 v2.3.0
	github.com/cncf/xds/go v0.0.0-20251022180443-0feb69152e9f
	github.com/envoyproxy/go-control-plane v0.13.5-0.20251024222203-75eaa193e329
	github.com/envoyproxy/go-control-plane/envoy v1.35.0
	github.com/golang/glog v1.2.5
	github.com/golang/protobuf v1.5.4
	github.com/google/go-cmp v0.7.0
	github.com/google/uuid v1.6.0
	github.com/spiffe/go-spiffe/v2 v2.6.0
	go.opentelemetry.io/contrib/detectors/gcp v1.38.0
	go.opentelemetry.io/otel v1.38.0
	go.opentelemetry.io/otel/metric v1.38.0
	go.opentelemetry.io/otel/sdk v1.38.0
	go.opentelemetry.io/otel/sdk/metric v1.38.0
	go.opentelemetry.io/otel/trace v1.38.0
	golang.org/x/net v0.46.0
	golang.org/x/oauth2 v0.32.0
	golang.org/x/sync v0.17.0
	golang.org/x/sys v0.37.0
	gonum.org/v1/gonum v0.16.0
	google.golang.org/genproto/googleapis/rpc v0.0.0-20251022142026-3a174f9686a8
	google.golang.org/protobuf v1.36.10
)

require (
	cel.dev/expr v0.24.0 // indirect
	cloud.google.com/go/compute/metadata v0.9.0 // indirect
	github.com/GoogleCloudPlatform/opentelemetry-operations-go/detectors/gcp v1.30.0 // indirect
	github.com/envoyproxy/go-control-plane/ratelimit v0.1.0 // indirect
	github.com/envoyproxy/protoc-gen-validate v1.2.1 // indirect
	github.com/go-jose/go-jose/v4 v4.1.3 // indirect
	github.com/go-logr/logr v1.4.3 // indirect
	github.com/go-logr/stdr v1.2.2 // indirect
	github.com/planetscale/vtprotobuf v0.6.1-0.20240319094008-0393e58bdf10 // indirect
	go.opentelemetry.io/auto/sdk v1.2.1 // indirect
	golang.org/x/text v0.30.0 // indirect
	google.golang.org/genproto/googleapis/api v0.0.0-20251022142026-3a174f9686a8 // indirect
)

// v1.74.0 was published prematurely with known issues.
retract [v1.74.0, v1.74.1]
