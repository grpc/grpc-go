all: vet test testrace

build:
	go build github.com/arshanvit/grpc-go/...

clean:
	go clean -i github.com/arshanvit/grpc-go/...

deps:
	GO111MODULE=on go get -d -v github.com/arshanvit/grpc-go/...

proto:
	@ if ! which protoc > /dev/null; then \
		echo "error: protoc not installed" >&2; \
		exit 1; \
	fi
	go generate github.com/arshanvit/grpc-go/...

test:
	go test -cpu 1,4 -timeout 7m github.com/arshanvit/grpc-go/...

testsubmodule:
	cd security/advancedtls && go test -cpu 1,4 -timeout 7m github.com/arshanvit/grpc-go/security/advancedtls/...
	cd security/authorization && go test -cpu 1,4 -timeout 7m github.com/arshanvit/grpc-go/security/authorization/...

testrace:
	go test -race -cpu 1,4 -timeout 7m github.com/arshanvit/grpc-go/...

testdeps:
	GO111MODULE=on go get -d -v -t github.com/arshanvit/grpc-go/...

vet: vetdeps
	./vet.sh

vetdeps:
	./vet.sh -install

.PHONY: \
	all \
	build \
	clean \
	proto \
	test \
	testrace \
	vet \
	vetdeps
