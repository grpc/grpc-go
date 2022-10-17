FROM golang:1.16

ENV GRPC_GO_LOG_SEVERITY_LEVEL info
ENV GRPC_GO_LOG_VERBOSITY_LEVEL 2

# Do I need to install google cloud trace, logging, monitoring and auth or will this be part of
# the kubernetes deployment and I can just package my gRPC Client/Server?

WORKDIR /grpc-go

# what does this do and how does it relate to
# the WORKDIR set above
COPY ..

RUN go build -o examples/route_guide/o11y-server/ examples/route_guide/o11y-server/server.go
RUN go build -o examples/route_guide/o11y-client/ examples/route_guide/o11y-client/client.go

# EXPOSE 10000?

CMD ["/bin/sleep", "inf"]