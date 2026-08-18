/*
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
 */

package opentelemetry

import (
	estats "google.golang.org/grpc/experimental/stats"
)

const (
	// TCPConnectionsCreatedMetricName is the total number of TCP connections established.
	TCPConnectionsCreatedMetricName string = "grpc.tcp.connections_created"
	// TCPConnectionCountMetricName is the current number of active TCP connections.
	TCPConnectionCountMetricName string = "grpc.tcp.connection_count"
	// TCPMinRTTMetricName is the minimum round-trip time of a TCP connection.
	TCPMinRTTMetricName string = "grpc.tcp.min_rtt"
	// TCPPacketsRetransmittedMetricName is the total number of packets retransmitted.
	TCPPacketsRetransmittedMetricName string = "grpc.tcp.packets_retransmitted"
	// TCPRecurringRetransmitsMetricName is the total number of recurring retransmits.
	TCPRecurringRetransmitsMetricName string = "grpc.tcp.recurring_retransmits"
	// TCPBytesSentMetricName is the total number of bytes sent at TCP layer.
	TCPBytesSentMetricName string = "grpc.tcp.bytes_sent"
	// TCPSyscallWritesMetricName is the total number of write syscalls.
	TCPSyscallWritesMetricName string = "grpc.tcp.syscall_writes"
	// TCPSyscallReadsMetricName is the total number of read syscalls.
	TCPSyscallReadsMetricName string = "grpc.tcp.syscall_reads"
)

var (
	// TCPConnectionsCreatedHandle is the handle for "grpc.tcp.connections_created".
	TCPConnectionsCreatedHandle = estats.RegisterInt64Count(estats.MetricDescriptor{
		Name:        TCPConnectionsCreatedMetricName,
		Description: "Total number of TCP connections created.",
		Unit:        "{connection}",
		Default:     false,
	})
	// TCPConnectionCountHandle is the handle for "grpc.tcp.connection_count".
	TCPConnectionCountHandle = estats.RegisterInt64UpDownCount(estats.MetricDescriptor{
		Name:        TCPConnectionCountMetricName,
		Description: "Current number of active TCP connections.",
		Unit:        "{connection}",
		Default:     false,
	})
	// TCPMinRTTHandle is the handle for "grpc.tcp.min_rtt".
	TCPMinRTTHandle = estats.RegisterFloat64Histo(estats.MetricDescriptor{
		Name:        TCPMinRTTMetricName,
		Description: "Minimum round-trip time of a TCP connection in seconds.",
		Unit:        "s",
		Bounds:      DefaultLatencyBounds,
		Default:     false,
	})
	// TCPPacketsRetransmittedHandle is the handle for "grpc.tcp.packets_retransmitted".
	TCPPacketsRetransmittedHandle = estats.RegisterInt64Count(estats.MetricDescriptor{
		Name:        TCPPacketsRetransmittedMetricName,
		Description: "Total number of TCP packets retransmitted.",
		Unit:        "{packet}",
		Default:     false,
	})
	// TCPRecurringRetransmitsHandle is the handle for "grpc.tcp.recurring_retransmits".
	TCPRecurringRetransmitsHandle = estats.RegisterInt64Count(estats.MetricDescriptor{
		Name:        TCPRecurringRetransmitsMetricName,
		Description: "Total number of TCP recurring retransmits.",
		Unit:        "{packet}",
		Default:     false,
	})
	// TCPBytesSentHandle is the handle for "grpc.tcp.bytes_sent".
	TCPBytesSentHandle = estats.RegisterInt64Count(estats.MetricDescriptor{
		Name:        TCPBytesSentMetricName,
		Description: "Total number of bytes sent at TCP layer.",
		Unit:        "By",
		Default:     false,
	})
	// TCPSyscallWritesHandle is the handle for "grpc.tcp.syscall_writes".
	TCPSyscallWritesHandle = estats.RegisterInt64Count(estats.MetricDescriptor{
		Name:        TCPSyscallWritesMetricName,
		Description: "Total number of TCP write syscalls.",
		Unit:        "{syscall}",
		Default:     false,
	})
	// TCPSyscallReadsHandle is the handle for "grpc.tcp.syscall_reads".
	TCPSyscallReadsHandle = estats.RegisterInt64Count(estats.MetricDescriptor{
		Name:        TCPSyscallReadsMetricName,
		Description: "Total number of TCP read syscalls.",
		Unit:        "{syscall}",
		Default:     false,
	})
)
