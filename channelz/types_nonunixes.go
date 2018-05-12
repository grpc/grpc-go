// +build !darwin,!dragonfly,!freebsd,!linux,!netbsd,!openbsd,!solaris

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

package channelz

// The three types of dummy defined here are to make channel/service package build
// under environment where x/sys/ doesn't have a definition for Linger, Timeval,
// and TCPInfo.

type tcpInfoDummy struct {
	State          uint8
	Ca_state       uint8
	Retransmits    uint8
	Probes         uint8
	Backoff        uint8
	Options        uint8
	Rto            uint32
	Ato            uint32
	Snd_mss        uint32
	Rcv_mss        uint32
	Unacked        uint32
	Sacked         uint32
	Lost           uint32
	Retrans        uint32
	Fackets        uint32
	Last_data_sent uint32
	Last_ack_sent  uint32
	Last_data_recv uint32
	Last_ack_recv  uint32
	Pmtu           uint32
	Rcv_ssthresh   uint32
	Rtt            uint32
	Rttvar         uint32
	Snd_ssthresh   uint32
	Snd_cwnd       uint32
	Advmss         uint32
	Reordering     uint32
}

type lingerDummy struct {
	Onoff  int32
	Linger int32
}

type timevalDummy struct {
	Sec  int32
	Usec int32
}

// SocketOptionData defines the struct to hold socket option data, and related
// getter function to obtain info from fd.
// Windows OS doesn't support Socket Option
type SocketOptionData struct {
	Linger      *lingerDummy
	RecvTimeout *timevalDummy
	SendTimeout *timevalDummy
	TCPInfo     *tcpInfoDummy
}

// Getsockopt defines the function to get socket options requested by channelz.
// It is to be passed to syscall.RawConn.Control().
// Windows OS doesn't support Socket Option
func (s *SocketOptionData) Getsockopt(fd uintptr) {}
