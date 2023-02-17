module google.golang.org/grpc/gcp/observability

go 1.17

require (
	cloud.google.com/go/logging v1.6.1
	contrib.go.opencensus.io/exporter/stackdriver v0.13.12
	github.com/google/go-cmp v0.5.9
	github.com/google/uuid v1.3.0
	go.opencensus.io v0.24.0
	golang.org/x/oauth2 v0.4.0
	google.golang.org/grpc v1.52.0
	google.golang.org/grpc/stats/opencensus v0.0.0-20230222033013-5353eaa44095
)

require (
	cloud.google.com/go v0.107.0 // indirect
	cloud.google.com/go/compute v1.15.1 // indirect
	cloud.google.com/go/compute/metadata v0.2.3 // indirect
	cloud.google.com/go/longrunning v0.3.0 // indirect
	cloud.google.com/go/monitoring v1.8.0 // indirect
	cloud.google.com/go/trace v1.4.0 // indirect
	github.com/aws/aws-sdk-go v1.44.162 // indirect
	github.com/census-instrumentation/opencensus-proto v0.4.1 // indirect
	github.com/golang/groupcache v0.0.0-20210331224755-41bb18bfe9da // indirect
	github.com/golang/protobuf v1.5.2 // indirect
	github.com/googleapis/enterprise-certificate-proxy v0.2.0 // indirect
	github.com/googleapis/gax-go/v2 v2.7.0 // indirect
	github.com/jmespath/go-jmespath v0.4.0 // indirect
	github.com/prometheus/prometheus v2.5.0+incompatible // indirect
	golang.org/x/net v0.5.0 // indirect
	golang.org/x/sync v0.1.0 // indirect
	golang.org/x/sys v0.4.0 // indirect
	golang.org/x/text v0.6.0 // indirect
	google.golang.org/api v0.103.0 // indirect
	google.golang.org/appengine v1.6.7 // indirect
	google.golang.org/genproto v0.0.0-20230110181048-76db0878b65f // indirect
	google.golang.org/protobuf v1.28.1 // indirect
)

replace google.golang.org/grpc => ../../

replace google.golang.org/grpc/stats/opencensus => ../../stats/opencensus
