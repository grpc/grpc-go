module google.golang.org/grpc

go 1.25.0

require (
	cloud.google.com/go/auth v0.23.1
	cloud.google.com/go/compute/metadata v0.9.0
	github.com/cespare/xxhash/v2 v2.3.0
	github.com/cncf/xds/go v0.0.0-20260202195803-dba9d589def2
	github.com/envoyproxy/go-control-plane v0.14.0
	github.com/envoyproxy/go-control-plane/envoy v1.39.0
	github.com/golang/glog v1.2.5
	github.com/golang/protobuf v1.5.4
	github.com/google/go-cmp v0.7.0
	github.com/google/uuid v1.6.0
	github.com/spiffe/go-spiffe/v2 v2.8.1
	go.opentelemetry.io/contrib/detectors/gcp v1.45.0
	go.opentelemetry.io/otel v1.45.0
	go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracegrpc v1.45.0
	go.opentelemetry.io/otel/metric v1.45.0
	go.opentelemetry.io/otel/sdk v1.45.0
	go.opentelemetry.io/otel/sdk/metric v1.45.0
	go.opentelemetry.io/otel/trace v1.45.0
	golang.org/x/net v0.58.0
	golang.org/x/oauth2 v0.36.0
	golang.org/x/sync v0.22.0
	golang.org/x/sys v0.47.0
	gonum.org/v1/gonum v0.17.0
	google.golang.org/genproto/googleapis/rpc v0.0.0-20260817212433-ac3dfec99bb1
	google.golang.org/protobuf v1.36.12
)

require (
	cel.dev/expr v0.25.3 // indirect
	github.com/GoogleCloudPlatform/opentelemetry-operations-go/detectors/gcp v1.35.0 // indirect
	github.com/cenkalti/backoff/v5 v5.0.3 // indirect
	github.com/envoyproxy/go-control-plane/ratelimit v0.1.0 // indirect
	github.com/envoyproxy/protoc-gen-validate v1.3.3 // indirect
	github.com/felixge/httpsnoop v1.1.0 // indirect
	github.com/go-jose/go-jose/v4 v4.1.4 // indirect
	github.com/go-logr/logr v1.4.4 // indirect
	github.com/go-logr/stdr v1.2.2 // indirect
	github.com/google/s2a-go v0.1.9 // indirect
	github.com/googleapis/enterprise-certificate-proxy v0.3.21 // indirect
	github.com/googleapis/gax-go/v2 v2.23.0 // indirect
	github.com/grpc-ecosystem/grpc-gateway/v2 v2.29.0 // indirect
	github.com/planetscale/vtprotobuf v0.6.1-0.20240319094008-0393e58bdf10 // indirect
	go.opentelemetry.io/auto/sdk v1.2.1 // indirect
	go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp v0.70.0 // indirect
	go.opentelemetry.io/otel/exporters/otlp/otlptrace v1.45.0 // indirect
	go.opentelemetry.io/proto/otlp v1.11.0 // indirect
	golang.org/x/crypto v0.55.0 // indirect
	golang.org/x/text v0.41.0 // indirect
	google.golang.org/api v0.293.0 // indirect
	google.golang.org/genproto/googleapis/api v0.0.0-20260817212433-ac3dfec99bb1 // indirect
)

// v1.74.0 was published prematurely with known issues.
retract [v1.74.0, v1.74.1]
