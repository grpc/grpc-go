package main

import (
	"flag"
	"fmt"
	"math/rand"
	"net"
	"strconv"
	"strings"
	"sync"
	"time"

	"golang.org/x/net/context"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/grpclog"
	"google.golang.org/grpc/interop"
	testpb "google.golang.org/grpc/interop/grpc_testing"
	metricspb "google.golang.org/grpc/stress/grpc_testing"
)

var (
	serverAddressesPtr      = flag.String("server_addresses", "localhost:8080", "a list of server addresses")
	testCasesPtr            = flag.String("test_cases", "", "a list of test cases along with the relative weights")
	testDurationSecsPtr     = flag.Int("test_duration_secs", -1, "test duration in seconds")
	numChannelsPerServerPtr = flag.Int("num_channels_per_server", 1, "Number of channels (i.e connections) to each server")
	numStubsPerChannelPtr   = flag.Int("num_stubs_per_channel", 1, "Number of client stubs per each connection to server")
	metricsPortPtr          = flag.Int("metrics_port", 8081, "The port at which the stress client exposes QPS metrics")
)

// testCaseType is the type of test to be run
type testCaseType uint32

const (
	// emptyUnary is to make a unary RPC with empty request and response
	emptyUnary testCaseType = 0

	// largeUnary is to make a unary RPC with large payload in the request and response
	largeUnary testCaseType = 1

	// TODO largeCompressedUnary

	// clientStreaming is to make a client streaming RPC
	clientStreaming testCaseType = 3

	// serverStreaming is to make a server streaming RPC
	serverStreaming testCaseType = 4

	// emptyStream is to make a bi-directional streaming with zero message
	emptyStream testCaseType = 5

	// unknownTest means something is wrong
	unknownTest testCaseType = 6
)

var testCaseNameTypeMap = map[string]testCaseType{
	"empty_unary":      emptyUnary,
	"large_unary":      largeUnary,
	"client_streaming": clientStreaming,
	"server_streaming": serverStreaming,
	"empty_stream":     emptyStream,
}

var testCaseTypeNameMap = map[testCaseType]string{
	emptyUnary:      "empty_unary",
	largeUnary:      "large_unary",
	clientStreaming: "client_streaming",
	serverStreaming: "server_streaming",
	emptyStream:     "empty_stream",
}

func (t testCaseType) String() string {
	if s, ok := testCaseTypeNameMap[t]; ok {
		return s
	}
	return ""
}

// testCaseWithWeight contains the test case type and its weight.
type testCaseWithWeight struct {
	testCase testCaseType
	weight   int
}

func (test testCaseWithWeight) String() string {
	return fmt.Sprintf("testCaseType: %d, Weight: %d", test.testCase, test.weight)
}

// parseTestCases converts test case string to a list of struct testCaseWithWeight.
func parseTestCases(testCaseString string) ([]testCaseWithWeight, error) {
	testCaseStrings := strings.Split(testCaseString, ",")
	testCases := make([]testCaseWithWeight, len(testCaseStrings))
	for i, str := range testCaseStrings {
		temp := strings.Split(str, ":")
		if len(temp) < 2 {
			return nil, fmt.Errorf("invalid test case with weight: %s", str)
		}
		t, ok := testCaseNameTypeMap[temp[0]]
		if !ok {
			return nil, fmt.Errorf("unknown test type: %s", temp[0])
		}
		testCases[i].testCase = t
		w, err := strconv.Atoi(temp[1])
		if err != nil {
			return nil, fmt.Errorf("%v", err)
		}
		testCases[i].weight = w
	}
	return testCases, nil
}

// weightedRandomTestSelector defines a weighted random selector for test case types.
type weightedRandomTestSelector struct {
	tests       []testCaseWithWeight
	totalWeight int
}

// newWeightedRandomTestSelector constructs a weightedRandomTestSelector with the given list of testCaseWithWeight.
func newWeightedRandomTestSelector(tests []testCaseWithWeight) *weightedRandomTestSelector {
	var totalWeight int
	for _, t := range tests {
		totalWeight += t.weight
	}
	rand.Seed(time.Now().UnixNano())
	return &weightedRandomTestSelector{tests, totalWeight}
}

func (selector weightedRandomTestSelector) getNextTest() (testCaseType, error) {
	random := rand.Intn(selector.totalWeight)
	var weightSofar int
	for _, test := range selector.tests {
		weightSofar += test.weight
		if random < weightSofar {
			return test.testCase, nil
		}
	}
	return unknownTest, fmt.Errorf("no test case selected by weightedRandomTestSelector")
}

// gauge defines type for gauge.
type gauge struct {
	mutex sync.RWMutex
	val   int64
}

// Set updates the gauge value
func (g *gauge) set(v int64) {
	g.mutex.Lock()
	defer g.mutex.Unlock()
	g.val = v
}

func (g *gauge) get() int64 {
	g.mutex.RLock()
	defer g.mutex.RUnlock()
	return g.val
}

// Server implements metrics server functions.
type server struct {
	mutex  sync.RWMutex
	gauges map[string]*gauge
}

// newMetricsServer returns a new metrics server.
func newMetricsServer() *server {
	return &server{gauges: make(map[string]*gauge)}
}

// GetAllGauges returns all gauges.
func (s *server) GetAllGauges(in *metricspb.EmptyMessage, stream metricspb.MetricsService_GetAllGaugesServer) error {
	s.mutex.RLock()
	defer s.mutex.RUnlock()

	for name, gauge := range s.gauges {
		err := stream.Send(&metricspb.GaugeResponse{Name: name, Value: &metricspb.GaugeResponse_LongValue{gauge.get()}})
		if err != nil {
			return err
		}
	}
	return nil
}

// GetGauge returns the gauge for the given name.
func (s *server) GetGauge(ctx context.Context, in *metricspb.GaugeRequest) (*metricspb.GaugeResponse, error) {
	s.mutex.RLock()
	defer s.mutex.RUnlock()

	if g, ok := s.gauges[in.Name]; ok {
		return &metricspb.GaugeResponse{Name: in.Name, Value: &metricspb.GaugeResponse_LongValue{g.get()}}, nil
	}
	return nil, grpc.Errorf(codes.InvalidArgument, "gauge with name %s not found", in.Name)
}

// CreateGauge creates a guage using the given name in metrics server
func (s *server) createGauge(name string) (*gauge, error) {
	s.mutex.Lock()
	defer s.mutex.Unlock()

	grpclog.Printf("create gauge: %s", name)
	if _, ok := s.gauges[name]; ok {
		// gauge already exists.
		return nil, fmt.Errorf("gauge %s already exists", name)
	}
	var g gauge
	s.gauges[name] = &g
	return &g, nil
}

func startServer(server *server, port int) {
	lis, err := net.Listen("tcp", ":"+strconv.Itoa(port))
	if err != nil {
		grpclog.Fatalf("failed to listen: %v", err)
	}

	s := grpc.NewServer()
	metricspb.RegisterMetricsServiceServer(s, server)
	s.Serve(lis)

}

// stressClient defines client for stress test.
type stressClient struct {
	testID           int
	address          string
	testDurationSecs int
	selector         *weightedRandomTestSelector
	interopClient    testpb.TestServiceClient
}

// newStressClient construct a new stressClient.
func newStressClient(id int, addr string, conn *grpc.ClientConn, selector *weightedRandomTestSelector, testDurSecs int) *stressClient {
	client := testpb.NewTestServiceClient(conn)
	return &stressClient{testID: id, address: addr, selector: selector, testDurationSecs: testDurSecs, interopClient: client}
}

// mainLoop uses weightedRandomTestSelector to select test case and runs the tests.
func (c *stressClient) mainLoop(gauge *gauge) {
	var numCalls int64
	timeStarted := time.Now()
	for testEndTime := time.Now().Add(time.Duration(c.testDurationSecs) * time.Second); c.testDurationSecs < 0 || time.Now().Before(testEndTime); {
		test, err := c.selector.getNextTest()
		if err != nil {
			grpclog.Printf("%v", err)
			continue
		}
		switch test {
		case emptyUnary:
			interop.DoEmptyUnaryCall(c.interopClient)
		case largeUnary:
			interop.DoLargeUnaryCall(c.interopClient)
		case clientStreaming:
			interop.DoClientStreaming(c.interopClient)
		case serverStreaming:
			interop.DoServerStreaming(c.interopClient)
		case emptyStream:
			interop.DoEmptyStream(c.interopClient)
		default:
			grpclog.Fatal("Unsupported test case: %d", test)
		}
		numCalls++
		gauge.set(int64(float64(numCalls) / time.Since(timeStarted).Seconds()))
	}
}

func logParameterInfo(addresses []string, tests []testCaseWithWeight) {
	grpclog.Printf("server_addresses: %s", *serverAddressesPtr)
	grpclog.Printf("test_cases: %s", *testCasesPtr)
	grpclog.Printf("test_duration-secs: %d", *testDurationSecsPtr)
	grpclog.Printf("num_channels_per_server: %d", *numChannelsPerServerPtr)
	grpclog.Printf("num_stubs_per_channel: %d", *numStubsPerChannelPtr)
	grpclog.Printf("metrics_port: %d", *metricsPortPtr)

	for i, addr := range addresses {
		grpclog.Printf("%d. %s\n", i+1, addr)
	}
	for i, test := range tests {
		grpclog.Printf("%d. %v\n", i+1, test)
	}
}

func main() {
	flag.Parse()
	serverAddresses := strings.Split(*serverAddressesPtr, ",")
	testCases, err := parseTestCases(*testCasesPtr)
	if err != nil {
		grpclog.Fatalf("%v\n", err)
	}
	logParameterInfo(serverAddresses, testCases)
	testSelector := newWeightedRandomTestSelector(testCases)
	metricsServer := newMetricsServer()

	var wg sync.WaitGroup
	wg.Add(len(serverAddresses) * *numChannelsPerServerPtr * *numStubsPerChannelPtr)
	var clientIndex int
	for serverIndex, address := range serverAddresses {
		for connIndex := 0; connIndex < *numChannelsPerServerPtr; connIndex++ {
			conn, err := grpc.Dial(address, grpc.WithInsecure())
			if err != nil {
				grpclog.Fatalf("Fail to dial: %v", err)
			}
			defer conn.Close()
			for stubIndex := 0; stubIndex < *numStubsPerChannelPtr; stubIndex++ {
				clientIndex++
				client := newStressClient(clientIndex, address, conn, testSelector, *testDurationSecsPtr)
				buf := fmt.Sprintf("/stress_test/server_%d/channel_%d/stub_%d/qps", serverIndex+1, connIndex+1, stubIndex+1)
				go func() {
					defer wg.Done()
					if g, err := metricsServer.createGauge(buf); err != nil {
						grpclog.Fatalf("%v", err)
					} else {
						client.mainLoop(g)
					}
				}()
			}

		}
	}
	go startServer(metricsServer, *metricsPortPtr)
	wg.Wait()
	grpclog.Printf(" ===== ALL DONE ===== ")

}
