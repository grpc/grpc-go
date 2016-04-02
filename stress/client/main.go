package main

import (
	"flag"
	"fmt"
	"math/rand"
	"strconv"
	"strings"
	"sync"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/grpclog"
	"google.golang.org/grpc/interop"
	testpb "google.golang.org/grpc/interop/grpc_testing"
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

// WeightedRandomTestSelector defines a weighted random selector for test case types.
type WeightedRandomTestSelector struct {
	tests       []testCaseWithWeight
	totalWeight int
}

// newWeightedRandomTestSelector constructs a WeightedRandomTestSelector with the given list of testCaseWithWeight.
func newWeightedRandomTestSelector(tests []testCaseWithWeight) *WeightedRandomTestSelector {
	var totalWeight int
	for _, t := range tests {
		totalWeight += t.weight
	}
	rand.Seed(time.Now().UnixNano())
	return &WeightedRandomTestSelector{tests, totalWeight}
}

func (selector WeightedRandomTestSelector) getNextTest() (testCaseType, error) {
	random := rand.Intn(selector.totalWeight)
	var weightSofar int
	for _, test := range selector.tests {
		weightSofar += test.weight
		if random < weightSofar {
			return test.testCase, nil
		}
	}
	return unknownTest, fmt.Errorf("no test case selected by WeightedRandomTestSelector")
}

// StressClient defines client for stress test.
type StressClient struct {
	testID           int
	address          string
	testDurationSecs int
	selector         *WeightedRandomTestSelector
	interopClient    testpb.TestServiceClient
}

// newStressClient construct a new StressClient.
func newStressClient(id int, addr string, conn *grpc.ClientConn, selector *WeightedRandomTestSelector, testDurSecs int) *StressClient {
	client := testpb.NewTestServiceClient(conn)
	return &StressClient{testID: id, address: addr, selector: selector, testDurationSecs: testDurSecs, interopClient: client}
}

// MainLoop uses WeightedRandomTestSelector to select test case and runs the tests.
func (c *StressClient) MainLoop(buf string) {
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
					client.MainLoop(buf)
				}()
			}

		}
	}
	wg.Wait()
	grpclog.Printf(" ===== ALL DONE ===== ")
}
