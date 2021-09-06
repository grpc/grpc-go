all: vet test testrace

build:
	go build github.com/arshanvit/grpc/...

clean:
	go clean -i github.com/arshanvit/grpc/...

deps:
	GO111MODULE=on go get -d -v github.com/arshanvit/grpc/...

proto:
	@ if ! which protoc > /dev/null; then \
		echo "error: protoc not installed" >&2; \
		exit 1; \
	fi
	go generate github.com/arshanvit/grpc/...

test:
	go test -cpu 1,4 -timeout 7m github.com/arshanvit/grpc/...

testsubmodule:
	cd security/advancedtls && go test -cpu 1,4 -timeout 7m github.com/arshanvit/grpc/security/advancedtls/...
	cd security/authorization && go test -cpu 1,4 -timeout 7m github.com/arshanvit/grpc/security/authorization/...

testrace:
	go test -race -cpu 1,4 -timeout 7m github.com/arshanvit/grpc/...

testdeps:
	GO111MODULE=on go get -d -v -t github.com/arshanvit/grpc/...

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
