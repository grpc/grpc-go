#!/usr/bin/env bash
echo $TRAVIS_GO_VERSION
echo $TRAVIS_COMMIT_RANGE
IFS='...' read -r -a commits <<< "$TRAVIS_COMMIT_RANGE"
echo "base commit number:"
echo ${commits[0]}
echo "current commit number:"
echo ${commits[-1]}

if [ -d "benchmark/compare" ]; then
  echo "dir benchmark/compare exist"
  go get -d -v -t google.golang.org/grpc/...

  cp -r benchmark tmpbenchmark

  go test google.golang.org/grpc/benchmark/... -benchmem -bench=BenchmarkClient/Tracing-kbps_0-MTU_0-maxConcurrentCalls_1 | tee result1
  ls benchmark/compare/
  git reset --hard ${commits[0]}
  ls benchmark/compare/
  if [ -e "benchmark/compare/main.go" ]; then
    echo "after reset: dir benchmark/compare exist"
  else
    rm -r benchmark
    mv tmpbenchmark benchmark
  fi
  go test google.golang.org/grpc/benchmark/... -benchmem -bench=BenchmarkClient/Tracing-kbps_0-MTU_0-maxConcurrentCalls_1 | tee result2
  go run benchmark/compare/main.go result1 result2
fi
