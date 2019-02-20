all: vet test testrace

build: deps
	go build google.golang.org/grpc/...

clean:
	go clean -i google.golang.org/grpc/...

deps:
	go get -d -v google.golang.org/grpc/...

proto:
	@ if ! which protoc > /dev/null; then \
		echo "error: protoc not installed" >&2; \
		exit 1; \
	fi
	go generate google.golang.org/grpc/...

test: testdeps
	GRPC_GO_LOG_VERBOSITY_LEVEL=99 GRPC_GO_LOG_SEVERITY_LEVEL=warning go test -v -cpu 1,4 -timeout 7m google.golang.org/grpc/balancer/xds/ -count 10

testappengine: testappenginedeps
	GRPC_GO_LOG_VERBOSITY_LEVEL=99 GRPC_GO_LOG_SEVERITY_LEVEL=warning  goapp test -v -cpu 1,4 -timeout 7m google.golang.org/grpc/balancer/xds/ -count 10

testappenginedeps:
	goapp get -d -v -t -tags 'appengine appenginevm' google.golang.org/grpc/...

testdeps:
	go get -d -v -t google.golang.org/grpc/...

testrace: testdeps
	GRPC_GO_LOG_VERBOSITY_LEVEL=99 GRPC_GO_LOG_SEVERITY_LEVEL=warning go test -v -race -cpu 1,4 -timeout 7m google.golang.org/grpc/balancer/xds/ -count 10

updatedeps:
	go get -d -v -u -f google.golang.org/grpc/...

updatetestdeps:
	go get -d -v -t -u -f google.golang.org/grpc/...

vet: vetdeps
	./vet.sh

vetdeps:
	./vet.sh -install

.PHONY: \
	all \
	build \
	clean \
	deps \
	proto \
	test \
	testappengine \
	testappenginedeps \
	testdeps \
	testrace \
	updatedeps \
	updatetestdeps \
	vet \
	vetdeps
