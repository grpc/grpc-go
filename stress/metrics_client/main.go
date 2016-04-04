package main

import (
	"flag"
	"io"
	"time"

	"golang.org/x/net/context"
	"google.golang.org/grpc"
	"google.golang.org/grpc/grpclog"
	metricspb "google.golang.org/grpc/stress/grpc_testing"
)

var (
	metricsServerAddressPtr = flag.String("metrics_server_address", "", "The metrics server addresses in the fomrat <hostname>:<port>")
	totalOnlyPtr            = flag.Bool("total_only", false, "If true, this prints only the total value of all gauges")
)

const timeoutSeconds = 10

func printMetrics(client metricspb.MetricsServiceClient, totalOnly bool) {
	ctx, _ := context.WithTimeout(context.Background(), timeoutSeconds*time.Second)
	stream, err := client.GetAllGauges(ctx, &metricspb.EmptyMessage{})
	if err != nil {
		grpclog.Fatalf("failed to call GetAllGuages: %v", err)
	}

	var overallQPS int64
	var rpcStatus error
	for {
		gaugeResponse, err := stream.Recv()
		if err != nil {
			rpcStatus = err
			break
		}
		if _, ok := gaugeResponse.GetValue().(*metricspb.GaugeResponse_LongValue); ok {
			if !totalOnly {
				grpclog.Printf("%s: %d", gaugeResponse.Name, gaugeResponse.GetLongValue())
			}
			overallQPS += gaugeResponse.GetLongValue()
		} else {
			grpclog.Printf("gauge %s is not a long value", gaugeResponse.Name)
		}
	}
	grpclog.Printf("overall qps: %d", overallQPS)
	if rpcStatus != io.EOF {
		grpclog.Fatalf("failed to finish server streaming: %v", rpcStatus)
	}
}

func main() {
	flag.Parse()
	if len(*metricsServerAddressPtr) == 0 {
		grpclog.Fatalf("Cannot connect to the Metrics server. Please pass the address of the metrics server to connect to via the 'metrics_server_address' flag")
	}

	conn, err := grpc.Dial(*metricsServerAddressPtr, grpc.WithInsecure())
	if err != nil {
		grpclog.Fatalf("cannot connect to metrics server: %v", err)
	}
	defer conn.Close()

	c := metricspb.NewMetricsServiceClient(conn)
	printMetrics(c, *totalOnlyPtr)
}
