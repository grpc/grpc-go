//go:build linux

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
	"net"
	"syscall"

	"golang.org/x/sys/unix"
	"google.golang.org/grpc/internal/transport"
)

func init() {
	transport.SampleTCPStats = func(conn net.Conn) any {
		return getTCPStats(conn)
	}
}

type tcpStats struct {
	minRTT               float64 // in seconds
	packetsRetransmitted int64
	recurringRetransmits int64
	bytesSent            int64
	syscallWrites        int64
	syscallReads         int64
}

func getTCPStats(conn net.Conn) *tcpStats {
	if conn == nil {
		return nil
	}
	if sc, ok := conn.(*transport.StatsConn); ok {
		if saved := sc.SavedStats(); saved != nil {
			if ts, ok := saved.(*tcpStats); ok {
				return ts
			}
		}
		conn = sc.Conn
	}
	c, ok := conn.(syscall.Conn)
	if !ok {
		return nil
	}
	rawConn, err := c.SyscallConn()
	if err != nil {
		return nil
	}
	var tcpi *unix.TCPInfo
	err = rawConn.Control(func(fd uintptr) {
		if info, err := unix.GetsockoptTCPInfo(int(fd), syscall.SOL_TCP, syscall.TCP_INFO); err == nil {
			tcpi = info
		}
	})
	if err != nil || tcpi == nil {
		return nil
	}

	return &tcpStats{
		minRTT:               float64(tcpi.Min_rtt) / 1e6, // Convert microseconds to seconds
		packetsRetransmitted: int64(tcpi.Total_retrans),
		recurringRetransmits: int64(tcpi.Retransmits),
		bytesSent:            int64(tcpi.Bytes_sent),
		syscallWrites:        int64(tcpi.Segs_out),
		syscallReads:         int64(tcpi.Segs_in),
	}
}
