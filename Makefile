.PHONY: \
	all \
	deps \
	updatedeps \
	testdeps \
	updatetestdeps \
	build \
	proto \
	lint \
	pretest \
	test \
	testrace \
	clean \

all: test testrace

deps:
	go get -d -v google.golang.org/grpc/...

updatedeps:
	go get -d -v -u -f google.golang.org/grpc/...

testdeps:
	go get -d -v -t google.golang.org/grpc/...

updatetestdeps:
	go get -d -v -t -u -f google.golang.org/grpc/...

build: deps
	go build google.golang.org/grpc/...

proto:
	@ if ! which protoc > /dev/null; then \
		echo "error: protoc not installed" >&2; \
		exit 1; \
	fi
	go get -v github.com/golang/protobuf/protoc-gen-go
	for file in $$(git ls-files '*.proto'); do \
		protoc -I $$(dirname $$file) --go_out=plugins=grpc:$$(dirname $$file) $$file; \
	done

lint: testdeps
	go get -v github.com/golang/lint/golint
	for file in $$(git ls-files '*.go' | grep -v '\.pb\.go$$' | grep -v '_string\.go$$'); do \
		golint $$file; \
	done

pretest: lint

test: pretest
	go test -v -cpu 1,4 google.golang.org/grpc/...

testrace: pretest
	go test -v -race -cpu 1,4 google.golang.org/grpc/...

clean:
	go clean google.golang.org/grpc/...
