/*
 *
 * Copyright 2018 gRPC authors.
 *
 * Licensed under the Apache License, Version 2.0 (the "License");
 * you may not use this file except in compliance with the License.
 * You may obtain a copy of the License at
 *
 *     http://www.apache.org/licenses/LICENSE-2.0
 *
 * Unless required by applicable law or agreed to in writing, software
 * distributed under the License is distributed on an "AS IS" BASIS,
 * WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
 * See the License for the specific language governing permissions and
 * limitations under the License.
 *
 */

//go:generate /bin/sh -exu -o pipefail -c "tail -n +22 ./gen.go | /bin/sh -exu -o pipefail"
package core // import "google.golang.org/grpc/credentials/alts/core"
/*
function finish {
  rm -f proto/grpc_gcp/grpc/gcp/*.proto
  rm -f proto/grpc_gcp/grpc/gcp/*.go
  rmdir proto/grpc_gcp/grpc/gcp
  rmdir proto/grpc_gcp/grpc
}
trap finish EXIT

mkdir -p proto/grpc_gcp/grpc/gcp
curl https://raw.githubusercontent.com/grpc/grpc-proto/master/grpc/gcp/altscontext.proto > proto/grpc_gcp/grpc/gcp/altscontext.proto
curl https://raw.githubusercontent.com/grpc/grpc-proto/master/grpc/gcp/handshaker.proto > proto/grpc_gcp/grpc/gcp/handshaker.proto
curl https://raw.githubusercontent.com/grpc/grpc-proto/master/grpc/gcp/transport_security_common.proto > proto/grpc_gcp/grpc/gcp/transport_security_common.proto
protoc --go_out=plugins=grpc,paths=source_relative:proto/grpc_gcp/ -Iproto/grpc_gcp/ proto/grpc_gcp/grpc/gcp/*.proto
rm -f *.pb.go
mv proto/grpc_gcp/grpc/gcp/*.pb.go proto/grpc_gcp/
exit 0
*/
