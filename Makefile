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

test: testdeps
	go test -v -cpu 1,4 google.golang.org/grpc/...

testrace: testdeps
	go test -v -race -cpu 1,4 google.golang.org/grpc/...

clean:
	go clean google.golang.org/grpc/...

coverage: testdeps
	echo "mode: set" > acc.out
	for Dir in $(find ./* -maxdepth 10 -type d ); do \
        	if ls $Dir/*.go &> /dev/null; then \
            		go test -coverprofile=profile.out $Dir; \
            		if [ -f profile.out ] then \
                		cat profile.out | grep -v "mode: set" >> acc.out; \
            		fi \
		fi \
	done
	goveralls -coverprofile=acc.out -v google.golang.org/grpc/...
	rm -rf ./profile.out
	rm -rf ./acc.out
