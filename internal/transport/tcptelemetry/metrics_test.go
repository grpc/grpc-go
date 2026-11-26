/*
 *
 * Copyright 2026 gRPC authors.
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

package tcptelemetry

import (
	"net"
	"net/netip"
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
	estats "google.golang.org/grpc/experimental/stats"
	"google.golang.org/grpc/internal/grpctest"
	istats "google.golang.org/grpc/internal/stats"
	"google.golang.org/grpc/internal/testutils/stats"
	grpcstats "google.golang.org/grpc/stats"
)

type s struct {
	grpctest.Tester
}

func Test(t *testing.T) {
	grpctest.RunSubTests(t, s{})
}

var wantOptionalLabels = []string{
	"network.local.address",
	"network.local.port",
	"network.peer.address",
	"network.peer.port",
}

// wantDescriptors lists the expected descriptors (excluding Bounds, which are
// verified separately) for all TCP telemetry metrics defined in gRFC A80.
var wantDescriptors = []estats.MetricDescriptor{
	{
		Name:        "grpc.tcp.min_rtt",
		Description: "EXPERIMENTAL. Minimum RTT observed on the TCP connection.",
		Unit:        "s",
		Type:        estats.MetricTypeFloatHisto,
	},
	{
		Name:        "grpc.tcp.delivery_rate",
		Description: "EXPERIMENTAL. Delivery rate of the TCP connection when not application-limited.",
		Unit:        "By/s",
		Type:        estats.MetricTypeFloatHisto,
	},
	{
		Name:        "grpc.tcp.packets_sent",
		Description: "EXPERIMENTAL. Number of TCP data segments sent on the connection.",
		Unit:        "{packet}",
		Type:        estats.MetricTypeIntCount,
	},
	{
		Name:        "grpc.tcp.packets_retransmitted",
		Description: "EXPERIMENTAL. Total number of retransmitted TCP packets on the connection.",
		Unit:        "{packet}",
		Type:        estats.MetricTypeIntCount,
	},
	{
		Name:        "grpc.tcp.packets_spurious_retransmitted",
		Description: "EXPERIMENTAL. Number of spurious retransmitted TCP packets (DSACK duplicates) reported on the connection.",
		Unit:        "{packet}",
		Type:        estats.MetricTypeIntCount,
	},
	{
		Name:        "grpc.tcp.recurring_retransmits",
		Description: "EXPERIMENTAL. Number of recurring unrecovered TCP timeouts/retransmits at connection close.",
		Unit:        "{packet}",
		Type:        estats.MetricTypeIntCount,
	},
	{
		Name:        "grpc.tcp.bytes_sent",
		Description: "EXPERIMENTAL. Number of TCP data bytes sent (acknowledged or unacknowledged), including retransmitted bytes.",
		Unit:        "By",
		Type:        estats.MetricTypeIntCount,
	},
	{
		Name:        "grpc.tcp.bytes_retransmitted",
		Description: "EXPERIMENTAL. Number of TCP bytes retransmitted on the connection.",
		Unit:        "By",
		Type:        estats.MetricTypeIntCount,
	},
	{
		Name:        "grpc.tcp.connections_created",
		Description: "EXPERIMENTAL. Total number of TCP connections created.",
		Unit:        "{connection}",
		Type:        estats.MetricTypeIntCount,
	},
	{
		Name:        "grpc.tcp.connection_count",
		Description: "EXPERIMENTAL. Current number of active TCP connections.",
		Unit:        "{connection}",
		Type:        estats.MetricTypeIntUpDownCount,
	},
	{
		Name:        "grpc.tcp.syscall_writes",
		Description: "EXPERIMENTAL. Number of write syscalls performed on TCP connections.",
		Unit:        "{syscall}",
		Type:        estats.MetricTypeIntCount,
	},
	{
		Name:        "grpc.tcp.write_size",
		Description: "EXPERIMENTAL. Number of bytes offered in each write syscall on TCP connections.",
		Unit:        "By",
		Type:        estats.MetricTypeFloatHisto,
	},
	{
		Name:        "grpc.tcp.syscall_reads",
		Description: "EXPERIMENTAL. Number of read syscalls performed on TCP connections.",
		Unit:        "{syscall}",
		Type:        estats.MetricTypeIntCount,
	},
	{
		Name:        "grpc.tcp.read_size",
		Description: "EXPERIMENTAL. Number of bytes read in each read syscall on TCP connections.",
		Unit:        "By",
		Type:        estats.MetricTypeFloatHisto,
	},
	{
		Name:        "grpc.tcp.sender_latency",
		Description: "EXPERIMENTAL. TCP sender latency from packet scheduling to transmission.",
		Unit:        "s",
		Type:        estats.MetricTypeFloatHisto,
	},
	{
		Name:        "grpc.tcp.transfer_latency",
		Description: "EXPERIMENTAL. TCP transfer latency from transmission to acknowledgment.",
		Unit:        "s",
		Labels:      []string{"grpc.transfer_size"},
		Type:        estats.MetricTypeFloatHisto,
	},
}

// TestMetricDescriptors verifies that all TCP telemetry metrics are registered
// in the global metrics registry with the expected descriptors, and that none
// of them are enabled by default.
func (s) TestMetricDescriptors(t *testing.T) {
	if got, want := len(wantDescriptors), 16; got != want {
		t.Fatalf("len(wantDescriptors) = %d, want %d", got, want)
	}
	defaultMetrics := estats.DefaultMetrics.Metrics()
	for _, want := range wantDescriptors {
		t.Run(want.Name, func(t *testing.T) {
			want.OptionalLabels = wantOptionalLabels
			want.Default = false
			got := estats.DescriptorForMetric(want.Name)
			if got == nil {
				t.Fatalf("DescriptorForMetric(%q) = nil, want registered descriptor", want.Name)
			}
			if diff := cmp.Diff(want, *got, cmpopts.IgnoreFields(estats.MetricDescriptor{}, "Bounds"), cmpopts.EquateEmpty()); diff != "" {
				t.Errorf("DescriptorForMetric(%q) mismatch (-want +got):\n%s", want.Name, diff)
			}
			if defaultMetrics[want.Name] {
				t.Errorf("Metric %q is present in estats.DefaultMetrics, want it disabled by default", want.Name)
			}
		})
	}
}

// TestHandleDescriptors verifies that each package-level metric handle refers
// to the expected registered descriptor.
func (s) TestHandleDescriptors(t *testing.T) {
	handles := map[string]*estats.MetricDescriptor{
		"grpc.tcp.min_rtt":                        minRTT.Descriptor(),
		"grpc.tcp.delivery_rate":                  deliveryRate.Descriptor(),
		"grpc.tcp.packets_sent":                   packetsSent.Descriptor(),
		"grpc.tcp.packets_retransmitted":          packetsRetransmitted.Descriptor(),
		"grpc.tcp.packets_spurious_retransmitted": packetsSpuriousRetransmitted.Descriptor(),
		"grpc.tcp.recurring_retransmits":          recurringRetransmits.Descriptor(),
		"grpc.tcp.bytes_sent":                     bytesSent.Descriptor(),
		"grpc.tcp.bytes_retransmitted":            bytesRetransmitted.Descriptor(),
		"grpc.tcp.connections_created":            connectionsCreated.Descriptor(),
		"grpc.tcp.connection_count":               connectionCount.Descriptor(),
		"grpc.tcp.syscall_writes":                 syscallWrites.Descriptor(),
		"grpc.tcp.write_size":                     writeSize.Descriptor(),
		"grpc.tcp.syscall_reads":                  syscallReads.Descriptor(),
		"grpc.tcp.read_size":                      readSize.Descriptor(),
		"grpc.tcp.sender_latency":                 senderLatency.Descriptor(),
		"grpc.tcp.transfer_latency":               transferLatency.Descriptor(),
	}
	if got, want := len(handles), len(wantDescriptors); got != want {
		t.Fatalf("Number of metric handles = %d, want %d", got, want)
	}
	for name, d := range handles {
		if d.Name != name {
			t.Errorf("Handle for %q has descriptor name %q", name, d.Name)
		}
		if reg := estats.DescriptorForMetric(name); reg != d {
			t.Errorf("Handle descriptor for %q = %p, want registered descriptor %p", name, d, reg)
		}
	}
}

// TestRecordAllMetrics records a value for each metric handle against a test
// metrics recorder and verifies that the recorded value is observed. Records go
// through a MetricsRecorderList, which panics if the number of label values
// does not match the number of required plus optional labels.
func (s) TestRecordAllMetrics(t *testing.T) {
	tmr := stats.NewTestMetricsRecorder()
	mrl := istats.NewMetricsRecorderList([]grpcstats.Handler{tmr})
	labels := []string{"127.0.0.1", "1234", "10.0.0.1", "443"}

	minRTT.Record(mrl, 0.001, labels...)
	deliveryRate.Record(mrl, 2, labels...)
	packetsSent.Record(mrl, 3, labels...)
	packetsRetransmitted.Record(mrl, 4, labels...)
	packetsSpuriousRetransmitted.Record(mrl, 5, labels...)
	recurringRetransmits.Record(mrl, 6, labels...)
	bytesSent.Record(mrl, 7, labels...)
	bytesRetransmitted.Record(mrl, 8, labels...)
	connectionsCreated.Record(mrl, 9, labels...)
	connectionCount.Record(mrl, -1, labels...)
	syscallWrites.Record(mrl, 11, labels...)
	writeSize.Record(mrl, 12, labels...)
	syscallReads.Record(mrl, 13, labels...)
	readSize.Record(mrl, 14, labels...)
	senderLatency.Record(mrl, 0.015, labels...)
	transferLatency.Record(mrl, 0.016, append([]string{"1024"}, labels...)...)

	want := map[string]float64{
		"grpc.tcp.min_rtt":                        0.001,
		"grpc.tcp.delivery_rate":                  2,
		"grpc.tcp.packets_sent":                   3,
		"grpc.tcp.packets_retransmitted":          4,
		"grpc.tcp.packets_spurious_retransmitted": 5,
		"grpc.tcp.recurring_retransmits":          6,
		"grpc.tcp.bytes_sent":                     7,
		"grpc.tcp.bytes_retransmitted":            8,
		"grpc.tcp.connections_created":            9,
		"grpc.tcp.connection_count":               -1,
		"grpc.tcp.syscall_writes":                 11,
		"grpc.tcp.write_size":                     12,
		"grpc.tcp.syscall_reads":                  13,
		"grpc.tcp.read_size":                      14,
		"grpc.tcp.sender_latency":                 0.015,
		"grpc.tcp.transfer_latency":               0.016,
	}
	for name, wantVal := range want {
		got, ok := tmr.Metric(name)
		if !ok {
			t.Errorf("Metric %q not recorded", name)
			continue
		}
		if got != wantVal {
			t.Errorf("Metric %q = %v, want %v", name, got, wantVal)
		}
	}
}

// TestHistogramBounds verifies the bucket boundaries configured on each
// histogram metric.
func (s) TestHistogramBounds(t *testing.T) {
	tests := []struct {
		name        string
		handle      *estats.Float64HistoHandle
		wantBuckets int
		wantLast    float64
	}{
		{name: "min_rtt", handle: minRTT, wantBuckets: 100, wantLast: float64(1<<24) * 1e-6},
		{name: "delivery_rate", handle: deliveryRate, wantBuckets: 100, wantLast: float64(1 << 34)},
		{name: "write_size", handle: writeSize, wantBuckets: 20, wantLast: float64(8 << 20)},
		{name: "read_size", handle: readSize, wantBuckets: 20, wantLast: float64(8 << 20)},
		{name: "sender_latency", handle: senderLatency, wantBuckets: 20, wantLast: 1e6 * 1e-6},
		{name: "transfer_latency", handle: transferLatency, wantBuckets: 20, wantLast: 1e6 * 1e-6},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			bounds := test.handle.Descriptor().Bounds
			if len(bounds) != test.wantBuckets {
				t.Fatalf("len(Bounds) = %d, want %d", len(bounds), test.wantBuckets)
			}
			for i := 1; i < len(bounds); i++ {
				if bounds[i] <= bounds[i-1] {
					t.Errorf("Bounds not strictly increasing at index %d: %v <= %v", i, bounds[i], bounds[i-1])
				}
			}
			if got := bounds[len(bounds)-1]; got != test.wantLast {
				t.Errorf("Last bound = %v, want %v", got, test.wantLast)
			}
			if bounds[0] <= 0 {
				t.Errorf("First bound = %v, want > 0", bounds[0])
			}
		})
	}
}

func (s) TestExponentialBuckets(t *testing.T) {
	tests := []struct {
		name    string
		maxVal  int64
		buckets int
		scale   float64
		want    []float64
	}{
		{
			name:    "hand_computed",
			maxVal:  100,
			buckets: 5,
			scale:   1,
			want:    []float64{3, 8, 19, 44, 100},
		},
		{
			name:    "hand_computed_scaled",
			maxVal:  100,
			buckets: 5,
			scale:   0.5,
			want:    []float64{1.5, 4, 9.5, 22, 50},
		},
		{
			name:    "max_less_than_buckets",
			maxVal:  3,
			buckets: 5,
			scale:   2,
			want:    []float64{2, 4, 6},
		},
		{
			name:    "max_equal_to_buckets",
			maxVal:  5,
			buckets: 5,
			scale:   1,
			want:    []float64{1, 2, 3, 4, 5},
		},
		{
			name:    "max_one_more_than_buckets",
			maxVal:  6,
			buckets: 5,
			scale:   1,
			want:    []float64{2, 3, 4, 5, 6},
		},
		{
			name:    "max_zero",
			maxVal:  0,
			buckets: 5,
			scale:   1,
			want:    []float64{},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got := exponentialBuckets(test.maxVal, test.buckets, test.scale)
			if diff := cmp.Diff(test.want, got, cmpopts.EquateEmpty()); diff != "" {
				t.Errorf("exponentialBuckets(%d, %d, %v) mismatch (-want +got):\n%s", test.maxVal, test.buckets, test.scale, diff)
			}
		})
	}
}

// TestExponentialBucketsProperties verifies the general properties of
// exponentialBuckets for a range of inputs where maxVal > buckets.
func (s) TestExponentialBucketsProperties(t *testing.T) {
	tests := []struct {
		maxVal  int64
		buckets int
		scale   float64
	}{
		{maxVal: 1 << 24, buckets: 100, scale: 1e-6},
		{maxVal: 1 << 34, buckets: 100, scale: 1},
		{maxVal: 8 << 20, buckets: 20, scale: 1},
		{maxVal: 1e6, buckets: 20, scale: 1e-6},
		{maxVal: 101, buckets: 100, scale: 1},
		{maxVal: 1000, buckets: 2, scale: 1},
	}
	for _, test := range tests {
		got := exponentialBuckets(test.maxVal, test.buckets, test.scale)
		if len(got) != test.buckets {
			t.Errorf("exponentialBuckets(%d, %d, %v) returned %d buckets, want %d", test.maxVal, test.buckets, test.scale, len(got), test.buckets)
			continue
		}
		for i := 1; i < len(got); i++ {
			if got[i] <= got[i-1] {
				t.Errorf("exponentialBuckets(%d, %d, %v) not strictly increasing at index %d: %v <= %v", test.maxVal, test.buckets, test.scale, i, got[i], got[i-1])
			}
		}
		if last, want := got[len(got)-1], float64(test.maxVal)*test.scale; last != want {
			t.Errorf("exponentialBuckets(%d, %d, %v) last bound = %v, want %v", test.maxVal, test.buckets, test.scale, last, want)
		}
	}
}

// customAddr is a net.Addr implementation that is not a *net.TCPAddr, used to
// exercise the fallback parsing path of extractAddressLabels.
type customAddr struct {
	network string
	addr    string
}

func (a customAddr) Network() string { return a.network }
func (a customAddr) String() string  { return a.addr }

// parseIP parses s into a net.IP, panicking if s is not a valid IP address.
func parseIP(s string) net.IP {
	return netip.MustParseAddr(s).AsSlice()
}

func (s) TestExtractAddressLabels(t *testing.T) {
	tests := []struct {
		name   string
		local  net.Addr
		remote net.Addr
		want   [4]string
	}{
		{
			name:   "ipv4_tcp_addr",
			local:  &net.TCPAddr{IP: parseIP("127.0.0.1"), Port: 50051},
			remote: &net.TCPAddr{IP: net.IPv4(10, 0, 0, 1), Port: 443},
			want:   [4]string{"127.0.0.1", "50051", "10.0.0.1", "443"},
		},
		{
			name:   "ipv6_tcp_addr",
			local:  &net.TCPAddr{IP: parseIP("::1"), Port: 8080},
			remote: &net.TCPAddr{IP: parseIP("2001:db8::1"), Port: 9090},
			want:   [4]string{"::1", "8080", "2001:db8::1", "9090"},
		},
		{
			name:   "ipv6_tcp_addr_with_zone",
			local:  &net.TCPAddr{IP: parseIP("fe80::1"), Port: 8080, Zone: "eth0"},
			remote: &net.TCPAddr{IP: parseIP("fe80::2"), Port: 9090},
			want:   [4]string{"fe80::1%eth0", "8080", "fe80::2", "9090"},
		},
		{
			name:   "tcp_addr_nil_ip",
			local:  &net.TCPAddr{Port: 1234},
			remote: &net.TCPAddr{IP: parseIP("1.2.3.4"), Port: 0},
			want:   [4]string{"", "1234", "1.2.3.4", "0"},
		},
		{
			name:   "custom_addr_ipv4",
			local:  customAddr{network: "custom", addr: "192.168.1.1:1000"},
			remote: customAddr{network: "custom", addr: "example.com:443"},
			want:   [4]string{"192.168.1.1", "1000", "example.com", "443"},
		},
		{
			name:   "custom_addr_ipv6",
			local:  customAddr{network: "custom", addr: "[::1]:2000"},
			remote: customAddr{network: "custom", addr: "[fe80::1%eth0]:3000"},
			want:   [4]string{"::1", "2000", "fe80::1%eth0", "3000"},
		},
		{
			name:   "malformed_custom_addr",
			local:  customAddr{network: "custom", addr: "no-port"},
			remote: customAddr{network: "custom", addr: "::1:443"},
			want:   [4]string{"", "", "", ""},
		},
		{
			name:   "unix_addr",
			local:  &net.UnixAddr{Name: "/tmp/sock", Net: "unix"},
			remote: &net.UnixAddr{Name: "@", Net: "unix"},
			want:   [4]string{"", "", "", ""},
		},
		{
			name:   "nil_addrs",
			local:  nil,
			remote: nil,
			want:   [4]string{"", "", "", ""},
		},
		{
			name:   "typed_nil_tcp_addr",
			local:  (*net.TCPAddr)(nil),
			remote: &net.TCPAddr{IP: parseIP("10.1.1.1"), Port: 80},
			want:   [4]string{"", "", "10.1.1.1", "80"},
		},
		{
			name:   "nil_remote_only",
			local:  &net.TCPAddr{IP: parseIP("10.1.1.1"), Port: 80},
			remote: nil,
			want:   [4]string{"10.1.1.1", "80", "", ""},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got := extractAddressLabels(test.local, test.remote)
			if got != test.want {
				t.Errorf("extractAddressLabels(%v, %v) = %q, want %q", test.local, test.remote, got, test.want)
			}
		})
	}
}

func (s) TestExtractAddressLabelsOrderMatchesOptionalLabels(t *testing.T) {
	idx := map[string]int{
		"network.local.address": localAddressIdx,
		"network.local.port":    localPortIdx,
		"network.peer.address":  peerAddressIdx,
		"network.peer.port":     peerPortIdx,
	}
	for i, l := range optionalLabels {
		if idx[l] != i {
			t.Errorf("Index constant for label %q = %d, want %d", l, idx[l], i)
		}
	}
}

func (s) TestTransferSizeBucket(t *testing.T) {
	tests := []struct {
		writeLen      int
		wantBenchmark int
		wantLabel     string
	}{
		{writeLen: 0, wantBenchmark: 0, wantLabel: "1024"},
		{writeLen: 1, wantBenchmark: 1, wantLabel: "1024"},
		{writeLen: 1023, wantBenchmark: 1023, wantLabel: "1024"},
		{writeLen: 1024, wantBenchmark: 1024, wantLabel: "1024"},
		{writeLen: 8191, wantBenchmark: 1024, wantLabel: "1024"},
		{writeLen: 8192, wantBenchmark: 8192, wantLabel: "8192"},
		{writeLen: 65535, wantBenchmark: 8192, wantLabel: "8192"},
		{writeLen: 65536, wantBenchmark: 65536, wantLabel: "65536"},
		{writeLen: 262143, wantBenchmark: 65536, wantLabel: "65536"},
		{writeLen: 262144, wantBenchmark: 262144, wantLabel: "262144"},
		{writeLen: 2097151, wantBenchmark: 262144, wantLabel: "262144"},
		{writeLen: 2097152, wantBenchmark: 2097152, wantLabel: "2097152"},
		{writeLen: 4194304, wantBenchmark: 2097152, wantLabel: "2097152"},
	}
	for _, test := range tests {
		gotBenchmark, gotLabel := transferSizeBucket(test.writeLen)
		if gotBenchmark != test.wantBenchmark || gotLabel != test.wantLabel {
			t.Errorf("transferSizeBucket(%d) = (%d, %q), want (%d, %q)", test.writeLen, gotBenchmark, gotLabel, test.wantBenchmark, test.wantLabel)
		}
	}
}
