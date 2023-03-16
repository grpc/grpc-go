module google.golang.org/grpc/examples

go 1.17

require (
	github.com/cncf/xds/go v0.0.0-20230105202645-06c439db220b
	github.com/golang/protobuf v1.5.3
	golang.org/x/oauth2 v0.5.0
	google.golang.org/genproto v0.0.0-20230125152338-dcaf20b6aeaa
	google.golang.org/grpc v1.53.0-dev.0.20230315171901-a1e657ce53ba
	google.golang.org/grpc/gcp/observability v0.0.0-20230315201940-6f44ae89b1ab
	google.golang.org/protobuf v1.29.1

)

require (
	cloud.google.com/go v0.109.0 // indirect
	cloud.google.com/go/compute v1.18.0 // indirect
	cloud.google.com/go/compute/metadata v0.2.3 // indirect
	cloud.google.com/go/logging v1.6.1 // indirect
	cloud.google.com/go/longrunning v0.4.0 // indirect
	cloud.google.com/go/monitoring v1.12.0 // indirect
	cloud.google.com/go/trace v1.8.0 // indirect
	contrib.go.opencensus.io/exporter/stackdriver v0.13.12 // indirect
	github.com/aws/aws-sdk-go v1.44.162 // indirect
	github.com/census-instrumentation/opencensus-proto v0.4.1 // indirect
	github.com/cespare/xxhash/v2 v2.2.0 // indirect
	github.com/cncf/udpa/go v0.0.0-20220112060539-c52dc94e7fbe // indirect
	github.com/envoyproxy/go-control-plane v0.10.3 // indirect
	github.com/envoyproxy/protoc-gen-validate v0.9.1 // indirect
	github.com/golang/groupcache v0.0.0-20210331224755-41bb18bfe9da // indirect
	github.com/google/go-cmp v0.5.9 // indirect
	github.com/google/uuid v1.3.0 // indirect
	github.com/googleapis/enterprise-certificate-proxy v0.2.3 // indirect
	github.com/googleapis/gax-go/v2 v2.7.0 // indirect
	github.com/jmespath/go-jmespath v0.4.0 // indirect
	github.com/prometheus/prometheus v2.5.0+incompatible // indirect
	go.opencensus.io v0.24.0 // indirect
	golang.org/x/net v0.8.0 // indirect
	golang.org/x/sync v0.1.0 // indirect
	golang.org/x/sys v0.6.0 // indirect
	golang.org/x/text v0.8.0 // indirect
	google.golang.org/api v0.109.0 // indirect
	google.golang.org/appengine v1.6.7 // indirect
	google.golang.org/grpc/stats/opencensus v0.0.0-20230317183452-b638faff2204 // indirect
)

replace google.golang.org/grpc => ../

replace google.golang.org/grpc/gcp/observability => ../gcp/observability

replace google.golang.org/grpc/stats/opencensus => ../stats/opencensus
