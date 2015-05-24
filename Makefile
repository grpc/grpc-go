.PHONY: \
	all \
	deps \
	updatedeps \
	testdeps \
	updatetestdeps \
	build \
	lint \
	pretest \
	test \
	testrace \
	clean \

all: test testrace

deps:
	go get -d -v ./...

updatedeps:
	go get -d -v -u -f ./...

testdeps:
	go get -d -v -t ./...

updatetestdeps:
	go get -d -v -t -u -f ./...

build: deps
	go build ./...

lint: testdeps
	go get -v github.com/golang/lint/golint
	for file in $(shell git ls-files '*.go' | grep -v '\.pb\.go$$' | grep -v '_string\.go$$'); do \
		golint $$file; \
	done

pretest: lint

test: pretest
	go test -v -cpu 1,4 ./...

testrace: pretest
	go test -v -race -cpu 1,4 ./...

clean:
	go clean ./...
