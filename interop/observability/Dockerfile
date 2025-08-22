# Copyright 2022 gRPC authors.
#
# Licensed under the Apache License, Version 2.0 (the "License");
# you may not use this file except in compliance with the License.
# You may obtain a copy of the License at
#
#     http://www.apache.org/licenses/LICENSE-2.0
#
# Unless required by applicable law or agreed to in writing, software
# distributed under the License is distributed on an "AS IS" BASIS,
# WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
# See the License for the specific language governing permissions and
# limitations under the License.


#
# Stage 1: Build the interop test client and server
#

FROM golang:1.24-bullseye as build

WORKDIR /grpc-go
COPY . .

WORKDIR /grpc-go/interop/observability
RUN go build -o server/ server/server.go && \
    go build -o client/ client/client.go



#
# Stage 2:
#
# - Copy only the necessary files to reduce Docker image size.
# - Have an ENTRYPOINT script which will launch the interop test client or server
#   with the given parameters.
#

FROM golang:1.24-bullseye

ENV GRPC_GO_LOG_SEVERITY_LEVEL info
ENV GRPC_GO_LOG_VERBOSITY_LEVEL 2

WORKDIR /grpc-go/interop/observability/server
COPY --from=build /grpc-go/interop/observability/server/server .

WORKDIR /grpc-go/interop/observability/client
COPY --from=build /grpc-go/interop/observability/client/client .

WORKDIR /grpc-go/interop/observability
COPY --from=build /grpc-go/interop/observability/run.sh .

ENTRYPOINT ["/grpc-go/interop/observability/run.sh"]
