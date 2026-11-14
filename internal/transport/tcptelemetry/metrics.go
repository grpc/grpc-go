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

// Package tcptelemetry defines and records the experimental TCP telemetry
// metrics described in gRFC A80:
// https://github.com/grpc/proposal/blob/master/A80-tcp-telemetry.md
package tcptelemetry

import (
	"math"
	"net"
	"strconv"

	estats "google.golang.org/grpc/experimental/stats"
)

// optionalLabels are the optional labels attached to all TCP telemetry
// metrics, as specified in gRFC A80.
var optionalLabels = []string{
	"network.local.address",
	"network.local.port",
	"network.peer.address",
	"network.peer.port",
}

// Indices of the address labels returned by extractAddressLabels. They match
// the order of optionalLabels.
const (
	localAddressIdx = iota
	localPortIdx
	peerAddressIdx
	peerPortIdx
)

var (
	minRTT = estats.RegisterFloat64Histo(estats.MetricDescriptor{
		Name:           "grpc.tcp.min_rtt",
		Description:    "EXPERIMENTAL. Minimum RTT observed on the TCP connection.",
		Unit:           "s",
		OptionalLabels: optionalLabels,
		Default:        false,
		Bounds:         exponentialBuckets(1<<24, 100, 1e-6),
	})
	deliveryRate = estats.RegisterFloat64Histo(estats.MetricDescriptor{
		Name:           "grpc.tcp.delivery_rate",
		Description:    "EXPERIMENTAL. Delivery rate of the TCP connection when not application-limited.",
		Unit:           "By/s",
		OptionalLabels: optionalLabels,
		Default:        false,
		Bounds:         exponentialBuckets(1<<34, 100, 1.0),
	})
	packetsSent = estats.RegisterInt64Count(estats.MetricDescriptor{
		Name:           "grpc.tcp.packets_sent",
		Description:    "EXPERIMENTAL. Number of TCP data segments sent on the connection.",
		Unit:           "{packet}",
		OptionalLabels: optionalLabels,
		Default:        false,
	})
	packetsRetransmitted = estats.RegisterInt64Count(estats.MetricDescriptor{
		Name:           "grpc.tcp.packets_retransmitted",
		Description:    "EXPERIMENTAL. Total number of retransmitted TCP packets on the connection.",
		Unit:           "{packet}",
		OptionalLabels: optionalLabels,
		Default:        false,
	})
	packetsSpuriousRetransmitted = estats.RegisterInt64Count(estats.MetricDescriptor{
		Name:           "grpc.tcp.packets_spurious_retransmitted",
		Description:    "EXPERIMENTAL. Number of spurious retransmitted TCP packets (DSACK duplicates) reported on the connection.",
		Unit:           "{packet}",
		OptionalLabels: optionalLabels,
		Default:        false,
	})
	recurringRetransmits = estats.RegisterInt64Count(estats.MetricDescriptor{
		Name:           "grpc.tcp.recurring_retransmits",
		Description:    "EXPERIMENTAL. Number of recurring unrecovered TCP timeouts/retransmits at connection close.",
		Unit:           "{packet}",
		OptionalLabels: optionalLabels,
		Default:        false,
	})
	bytesSent = estats.RegisterInt64Count(estats.MetricDescriptor{
		Name:           "grpc.tcp.bytes_sent",
		Description:    "EXPERIMENTAL. Number of TCP data bytes sent (acknowledged or unacknowledged), including retransmitted bytes.",
		Unit:           "By",
		OptionalLabels: optionalLabels,
		Default:        false,
	})
	bytesRetransmitted = estats.RegisterInt64Count(estats.MetricDescriptor{
		Name:           "grpc.tcp.bytes_retransmitted",
		Description:    "EXPERIMENTAL. Number of TCP bytes retransmitted on the connection.",
		Unit:           "By",
		OptionalLabels: optionalLabels,
		Default:        false,
	})
	connectionsCreated = estats.RegisterInt64Count(estats.MetricDescriptor{
		Name:           "grpc.tcp.connections_created",
		Description:    "EXPERIMENTAL. Total number of TCP connections created.",
		Unit:           "{connection}",
		OptionalLabels: optionalLabels,
		Default:        false,
	})
	connectionCount = estats.RegisterInt64UpDownCount(estats.MetricDescriptor{
		Name:           "grpc.tcp.connection_count",
		Description:    "EXPERIMENTAL. Current number of active TCP connections.",
		Unit:           "{connection}",
		OptionalLabels: optionalLabels,
		Default:        false,
	})
	syscallWrites = estats.RegisterInt64Count(estats.MetricDescriptor{
		Name:           "grpc.tcp.syscall_writes",
		Description:    "EXPERIMENTAL. Number of write syscalls performed on TCP connections.",
		Unit:           "{syscall}",
		OptionalLabels: optionalLabels,
		Default:        false,
	})
	writeSize = estats.RegisterFloat64Histo(estats.MetricDescriptor{
		Name:           "grpc.tcp.write_size",
		Description:    "EXPERIMENTAL. Number of bytes offered in each write syscall on TCP connections.",
		Unit:           "By",
		OptionalLabels: optionalLabels,
		Default:        false,
		Bounds:         exponentialBuckets(8<<20, 20, 1.0),
	})
	syscallReads = estats.RegisterInt64Count(estats.MetricDescriptor{
		Name:           "grpc.tcp.syscall_reads",
		Description:    "EXPERIMENTAL. Number of read syscalls performed on TCP connections.",
		Unit:           "{syscall}",
		OptionalLabels: optionalLabels,
		Default:        false,
	})
	readSize = estats.RegisterFloat64Histo(estats.MetricDescriptor{
		Name:           "grpc.tcp.read_size",
		Description:    "EXPERIMENTAL. Number of bytes read in each read syscall on TCP connections.",
		Unit:           "By",
		OptionalLabels: optionalLabels,
		Default:        false,
		Bounds:         exponentialBuckets(8<<20, 20, 1.0),
	})
	senderLatency = estats.RegisterFloat64Histo(estats.MetricDescriptor{
		Name:           "grpc.tcp.sender_latency",
		Description:    "EXPERIMENTAL. TCP sender latency from packet scheduling to transmission.",
		Unit:           "s",
		OptionalLabels: optionalLabels,
		Default:        false,
		Bounds:         exponentialBuckets(1e6, 20, 1e-6),
	})
	transferLatency = estats.RegisterFloat64Histo(estats.MetricDescriptor{
		Name:           "grpc.tcp.transfer_latency",
		Description:    "EXPERIMENTAL. TCP transfer latency from transmission to acknowledgment.",
		Unit:           "s",
		Labels:         []string{"grpc.transfer_size"},
		OptionalLabels: optionalLabels,
		Default:        false,
		Bounds:         exponentialBuckets(1e6, 20, 1e-6),
	})
)

// exponentialBuckets returns histogram bucket boundaries matching gRPC C++'s
// ExponentialInt64HistogramShape(max, buckets), with each boundary multiplied
// by scale. See
// https://github.com/grpc/grpc/blob/master/src/core/telemetry/histogram.h.
//
// If maxVal <= buckets, the boundaries are 1, 2, ..., maxVal (scaled).
// Otherwise, exactly buckets strictly increasing boundaries are returned, with
// the last one equal to maxVal (scaled).
func exponentialBuckets(maxVal int64, buckets int, scale float64) []float64 {
	if maxVal <= int64(buckets) {
		bounds := make([]float64, 0, maxVal)
		for i := int64(0); i < maxVal; i++ {
			bounds = append(bounds, float64(i+1)*scale)
		}
		return bounds
	}
	intBounds := make([]int64, 0, buckets)
	firstBucket := int64(math.Ceil(math.Pow(float64(maxVal), 1.0/float64(buckets+1))))
	if firstBucket <= 1 {
		firstBucket = 1
	}
	intBounds = append(intBounds, firstBucket)
	for len(intBounds) < buckets {
		var nextb int64
		prevb := intBounds[len(intBounds)-1]
		if len(intBounds) == buckets-1 {
			nextb = maxVal
		} else {
			mul := math.Pow(float64(maxVal)/float64(prevb), 1.0/float64(buckets-len(intBounds)))
			nextb = int64(math.Ceil(float64(prevb) * mul))
		}
		if nextb <= prevb+1 {
			nextb = prevb + 1
		}
		intBounds = append(intBounds, nextb)
	}
	bounds := make([]float64, len(intBounds))
	for i, b := range intBounds {
		bounds[i] = float64(b) * scale
	}
	return bounds
}

// extractAddressLabels returns the values of the optional address labels for
// a connection, in the order [local address, local port, peer address, peer
// port]. Values that cannot be determined are left empty.
func extractAddressLabels(localAddr, remoteAddr net.Addr) [4]string {
	var labels [4]string
	labels[localAddressIdx], labels[localPortIdx] = splitAddr(localAddr)
	labels[peerAddressIdx], labels[peerPortIdx] = splitAddr(remoteAddr)
	return labels
}

// splitAddr returns the IP and port of addr. It handles *net.TCPAddr directly
// and falls back to parsing addr.String() as host:port for other types. In both
// cases, an IPv6 zone, if present, is kept in the returned IP (e.g.
// "fe80::1%eth0").
func splitAddr(addr net.Addr) (ip, port string) {
	if addr == nil {
		return "", ""
	}
	if tcpAddr, ok := addr.(*net.TCPAddr); ok {
		if tcpAddr == nil {
			return "", ""
		}
		if tcpAddr.IP != nil {
			ip = tcpAddr.IP.String()
			if tcpAddr.Zone != "" {
				ip += "%" + tcpAddr.Zone
			}
		}
		return ip, strconv.Itoa(tcpAddr.Port)
	}
	host, p, err := net.SplitHostPort(addr.String())
	if err != nil {
		return "", ""
	}
	return host, p
}

// transferSizeBucket maps the size of a write to the number of bytes that are
// benchmarked for transfer latency and the corresponding value of the
// grpc.transfer_size label, as specified by the table in gRFC A80.
func transferSizeBucket(writeLen int) (benchmarkSize int, label string) {
	switch {
	case writeLen < 1024:
		return writeLen, "1024"
	case writeLen < 8192:
		return 1024, "1024"
	case writeLen < 65536:
		return 8192, "8192"
	case writeLen < 262144:
		return 65536, "65536"
	case writeLen < 2097152:
		return 262144, "262144"
	default:
		return 2097152, "2097152"
	}
}
