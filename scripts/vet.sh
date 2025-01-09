#!/bin/bash

set -ex         # Exit on error; debugging enabled.
set -o pipefail # Fail a pipe if any sub-command fails.

source "$(dirname $0)/common.sh"

# Check to make sure it's safe to modify the user's git repo.
git status --porcelain | fail_on_output

# Undo any edits made by this script.
cleanup() {
  git reset --hard HEAD
}
trap cleanup EXIT

if [ -n "${GOROOT}" ]; then
  PATH="${GOROOT}/bin:${PATH}"
fi
PATH="${HOME}/go/bin:${PATH}"
go version

if [[ "$1" = "-install" ]]; then
  # Install the pinned versions as defined in module tools.
  pushd ./test/tools
  go install github.com/golangci/golangci-lint/cmd/golangci-lint
  popd
  exit 0
elif [[ "$#" -ne 0 ]]; then
  die "Unknown argument(s): $*"
fi

# - Ensure all source files contain a copyright message.
# (Done in two parts because Darwin "git grep" has broken support for compound
# exclusion matches.)
(grep -L "DO NOT EDIT" $(git grep -L "\(Copyright [0-9]\{4,\} gRPC authors\)" -- '*.go') || true) | fail_on_output

# - Make sure all tests in grpc and grpc/test use leakcheck via Teardown.
not grep 'func Test[^(]' -- *_test.go
not grep 'func Test[^(]' -- test/*.go

# - Check for typos in test function names
git grep 'func (s) ' -- "*_test.go" | not grep -v 'func (s) Test'
git grep 'func [A-Z]' -- "*_test.go" | not grep -v 'func Test\|Benchmark\|Example'

# - Do not use time.After except in tests.  It has the potential to leak the
#   timer since there is no way to stop it early.
git grep -l 'time.After(' -- "*.go" | not grep -v '_test.go\|soak_tests\|testutils'

# - Do not use "interface{}"; use "any" instead.
git grep -l 'interface{}' -- "*.go" 2>&1 | not grep -v '\.pb\.go\|protoc-gen-go-grpc\|grpc_testing_not_regenerated'

# - Do not call grpclog directly. Use grpclog.Component instead.
git grep -l -e 'grpclog.I' --or -e 'grpclog.W' --or -e 'grpclog.E' --or -e 'grpclog.F' --or -e 'grpclog.V' -- "*.go" | not grep -v '^grpclog/component.go\|^internal/grpctest/tlogger_test.go\|^internal/grpclog/prefix_logger.go'

# - Ensure that the deprecated protobuf dependency is not used.
not git grep "\"github.com/golang/protobuf/*" -- "*.go" ':(exclude)testdata/grpc_testing_not_regenerated/*'

# - Ensure all usages of grpc_testing package are renamed when importing.
not git grep "\(import \|^\s*\)\"google.golang.org/grpc/interop/grpc_testing" -- "*.go"

# - Ensure that no trailing spaces are found.
not git grep '[[:blank:]]$'

# - Ensure that all files have a terminating newline.
git ls-files | not xargs -I {} sh -c '[ -n "$(tail -c 1 "{}" 2>/dev/null)" ] && echo "{}: No terminating new line found"' | fail_on_output

# - Ensure that no tabs are found in markdown files.
not git grep $'\t' -- '*.md'

# - Ensure all xds proto imports are renamed to *pb or *grpc.
git grep '"github.com/envoyproxy/go-control-plane/envoy' -- '*.go' ':(exclude)*.pb.go' | not grep -v 'pb "\|grpc "'

# - Ensure all context usages are done with timeout.
# Context tests under benchmark are excluded as they are testing the performance of context.Background() and context.TODO().
# TODO: Remove the exclusions once the tests are updated to use context.WithTimeout().
# See https://github.com/grpc/grpc-go/issues/7304
git grep -e 'context.Background()' --or -e 'context.TODO()' -- "*_test.go" | grep -v "benchmark/primitives/context_test.go" | grep -v "credential
s/google" | grep -v "internal/transport/" | grep -v "xds/internal/" | grep -v "security/advancedtls" | grep -v 'context.WithTimeout(' | not grep -v 'context.WithCancel('

# Disallow usage of net.ParseIP in favour of netip.ParseAddr as the former
# can't parse link local IPv6 addresses.
not git grep 'net.ParseIP' -- '*.go'

GOLANGCI_LINT_CONFIG="$(readlink -f $(dirname $0))/.golangci-lint.yaml"

# - gofmt, goimports, go vet, go mod tidy.
# Perform these checks on each module inside gRPC.
for MOD_FILE in $(find . -name 'go.mod'); do
  MOD_DIR=$(dirname ${MOD_FILE})
  pushd ${MOD_DIR}

  go mod tidy -compat=1.22
  git status --porcelain 2>&1 | fail_on_output || \
    (git status; git --no-pager diff; exit 1)

  golangci-lint run --config "$GOLANGCI_LINT_CONFIG" ./...
  popd
done

echo SUCCESS
