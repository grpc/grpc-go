#!/bin/bash

set -ex         # Exit on error; debugging enabled.
set -o pipefail # Fail a pipe if any sub-command fails.

source "$(dirname $0)/vet-common.sh"

# Check to make sure it's safe to modify the user's git repo.
git status --porcelain | fail_on_output

# noret_grep will return 0 if zero or more lines were selected, and >1 if an
# error occurred. Suppresses grep's return code of 1 when there are no matches
# (for eg, empty file).
noret_grep() {
  grep "$@" || [[ $? == 1 ]]
}

# Undo any edits made by this script.
cleanup() {
  git reset --hard HEAD
}
trap cleanup EXIT

PATH="${HOME}/go/bin:${GOROOT}/bin:${PATH}"
go version

if [[ "$1" = "-install" ]]; then
  # Install the pinned versions as defined in module tools.
  pushd ./test/tools
  go install \
    golang.org/x/tools/cmd/goimports \
    honnef.co/go/tools/cmd/staticcheck \
    github.com/client9/misspell/cmd/misspell
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
not grep 'func Test[^(]' *_test.go
not grep 'func Test[^(]' test/*.go

# - Check for typos in test function names
git grep --quiet 'func (s) ' -- "*_test.go" | not grep -v 'func (s) Test'
git grep --quiet 'func [A-Z]' -- "*_test.go" | not grep -v 'func Test\|Benchmark\|Example'

# - Do not use time.After except in tests.  It has the potential to leak the
#   timer since there is no way to stop it early.
git grep --quiet -l 'time.After(' -- "*.go" | not grep -v '_test.go\|test_utils\|testutils'

# - Do not import math/rand for real library code.  Use internal/grpcrand for
#   thread safety.
git grep --quiet -l '"math/rand"' -- "*.go" 2>&1 | not grep -v '^examples\|^interop/stress\|grpcrand\|^benchmark\|wrr_test'

# - Do not use "interface{}"; use "any" instead.
git grep --quiet -l 'interface{}' -- "*.go" 2>&1 | not grep -v '\.pb\.go\|protoc-gen-go-grpc\|grpc_testing_not_regenerate'

# - Do not call grpclog directly. Use grpclog.Component instead.
git grep --quiet -l -e 'grpclog.I' --or -e 'grpclog.W' --or -e 'grpclog.E' --or -e 'grpclog.F' --or -e 'grpclog.V' -- "*.go" | not grep -v '^grpclog/component.go\|^internal/grpctest/tlogger_test.go'

# - Ensure that the deprecated protobuf dependency is not used.
not git grep --quiet "\"github.com/golang/protobuf/*" -- "*.go" ':(exclude)reflection/test/grpc_testing_not_regenerate/*'

# - Ensure all usages of grpc_testing package are renamed when importing.
not git grep --quiet "\(import \|^\s*\)\"google.golang.org/grpc/interop/grpc_testing" -- "*.go"

# - Ensure all xds proto imports are renamed to *pb or *grpc.
git grep --quiet '"github.com/envoyproxy/go-control-plane/envoy' -- '*.go' ':(exclude)*.pb.go' | not grep -v 'pb "\|grpc "'

misspell -error .

# - gofmt, goimports, go vet, go mod tidy.
# Perform these checks on each module inside gRPC.
for MOD_FILE in $(find . -name 'go.mod'); do
  MOD_DIR=$(dirname ${MOD_FILE})
  pushd ${MOD_DIR}
  go vet -all ./... | fail_on_output
  gofmt -s -d -l . 2>&1 | fail_on_output
  goimports -l . 2>&1 | not grep -vE "\.pb\.go"

  go mod tidy -compat=1.19
  git status --porcelain 2>&1 | fail_on_output || \
    (git status; git --no-pager diff; exit 1)
  
  # - Collection of static analysis checks
  SC_OUT="$(mktemp)"
  staticcheck -go 1.19 -checks 'all' ./... >"${SC_OUT}" || true

  # Error for anything other than checks that need exclusions.
  noret_grep -v "(ST1000)" "${SC_OUT}" | noret_grep -v "(SA1019)" | noret_grep -v "(ST1003)" | noret_grep -v "(ST1019)\|\(other import of\)" | not grep -v "(SA4000)"

  # Exclude underscore checks for generated code.
  noret_grep "(ST1003)" "${SC_OUT}" | not grep -v '\(.pb.go:\)\|\(code_string_test.go:\)\|\(grpc_testing_not_regenerate\)'

  # Error for duplicate imports not including grpc protos.
  noret_grep "(ST1019)\|\(other import of\)" "${SC_OUT}" | not grep -Fv 'XXXXX PleaseIgnoreUnused
channelz/grpc_channelz_v1"
go-control-plane/envoy
grpclb/grpc_lb_v1"
health/grpc_health_v1"
interop/grpc_testing"
orca/v3"
proto/grpc_gcp"
proto/grpc_lookup_v1"
examples/features/proto/echo"
reflection/grpc_reflection_v1"
reflection/grpc_reflection_v1alpha"
XXXXX PleaseIgnoreUnused'

  # Error for any package comments not in generated code.
  noret_grep "(ST1000)" "${SC_OUT}" | not grep -v "\.pb\.go:"

  # Ignore a false positive when operands have side affectes.
  # TODO(https://github.com/dominikh/go-tools/issues/54): Remove this once the issue is fixed in staticcheck.
  noret_grep "(SA4000)" "${SC_OUT}" | not grep -ev "crl.go:\d*:\d*: identical expressions on the left and right side of the '||' operator (SA4000)"

  # Only ignore the following deprecated types/fields/functions and exclude
  # generated code.
  noret_grep "(SA1019)" "${SC_OUT}" | not grep -Fv 'XXXXX PleaseIgnoreUnused
XXXXX Protobuf related deprecation errors:
"github.com/golang/protobuf
.pb.go:
grpc_testing_not_regenerate
: ptypes.
proto.RegisterType
XXXXX gRPC internal usage deprecation errors:
"google.golang.org/grpc
: grpc.
: v1alpha.
: v1alphareflectionpb.
BalancerAttributes is deprecated:
CredsBundle is deprecated:
Metadata is deprecated: use Attributes instead.
NewSubConn is deprecated:
OverrideServerName is deprecated:
RemoveSubConn is deprecated:
SecurityVersion is deprecated:
Target is deprecated: Use the Target field in the BuildOptions instead.
UpdateAddresses is deprecated:
UpdateSubConnState is deprecated:
balancer.ErrTransientFailure is deprecated:
grpc/reflection/v1alpha/reflection.proto
SwitchTo is deprecated:
XXXXX xDS deprecated fields we support
.ExactMatch
.PrefixMatch
.SafeRegexMatch
.SuffixMatch
GetContainsMatch
GetExactMatch
GetMatchSubjectAltNames
GetPrefixMatch
GetSafeRegexMatch
GetSuffixMatch
GetTlsCertificateCertificateProviderInstance
GetValidationContextCertificateProviderInstance
XXXXX PleaseIgnoreUnused'
  popd
done

echo SUCCESS
