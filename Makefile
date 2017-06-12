all: test testrace

deps:
	go get -d -v google.golang.org/grpc/...

updatedeps:
	go get -d -v -u -f google.golang.org/grpc/...

testdeps:
	go get -d -v -t google.golang.org/grpc/...

benchdeps: testdeps
	go get -d -v golang.org/x/perf/cmd/benchstat

updatetestdeps:
	go get -d -v -t -u -f google.golang.org/grpc/...

build: deps
	go build google.golang.org/grpc/...

proto:
	@ if ! which protoc > /dev/null; then \
		echo "error: protoc not installed" >&2; \
		exit 1; \
	fi
	go get -u -v github.com/golang/protobuf/protoc-gen-go
	# use $$dir as the root for all proto files in the same directory
	for dir in $$(git ls-files '*.proto' | xargs -n1 dirname | uniq); do \
		protoc -I $$dir --go_out=plugins=grpc:$$dir $$dir/*.proto; \
	done
	cat grpclb/grpc_lb_v1/load_balancer.pb.go | \
		sed 's!^package.*!&;import lbpb "google.golang.org/grpc/grpclb/grpc_lb_v1"!' | \
		sed 's/LoadBalanceRequest/lbpb.LoadBalanceRequest/g' | \
		sed 's/LoadBalanceResponse/lbpb.LoadBalanceResponse/g' | \
		sed 's/package grpc_lb_v1/package grpclb/' | gofmt > grpclb/grpclb_load_balancer.pb.go
	rm grpclb/grpc_lb_v1/load_balancer.pb.go

test: testdeps
	go test -v -cpu 1,4 google.golang.org/grpc/...

testrace: testdeps
	go test -v -race -cpu 1,4 google.golang.org/grpc/...

benchmark: benchdeps
	go test google.golang.org/grpc/benchmark/... -benchmem -bench=. | tee /tmp/tmp.result && benchstat /tmp/tmp.result && rm /tmp/tmp.result

clean:
	go clean -i google.golang.org/grpc/...

coverage: testdeps
	./coverage.sh --coveralls

.PHONY: \
	all \
	deps \
	updatedeps \
	testdeps \
	updatetestdeps \
	build \
	proto \
	test \
	testrace \
	clean \
	coverage \
	benchdeps \
	benchmark
