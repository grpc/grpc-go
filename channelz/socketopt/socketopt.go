// +build linux
// +build go1.9

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

// Package socketopt defines the functionalities to get the socket options from
// a socket. Users must import this package to enable getting socket options in
// channelz service. Since AppEngine doesn't allow unsafe package usage (imported
// by package unix), this package should not be used in an AppEngine environment.
// And as build tags suggest, socket options in channelz service is only supported
// in linux environment with go version 1.9 or later.
package socketopt

import (
	"syscall"
	"time"

	"github.com/golang/protobuf/ptypes"
	durpb "github.com/golang/protobuf/ptypes/duration"
	"golang.org/x/sys/unix"
	"google.golang.org/grpc/channelz"
	channelzpb "google.golang.org/grpc/channelz/grpc_channelz_v1"
	"google.golang.org/grpc/channelz/service"
)

func init() {
	channelz.GetSocketOption = func(socket interface{}) channelz.SocketOptionData {
		c, ok := socket.(syscall.Conn)
		if !ok {
			return nil
		}
		data := &socketOptionData{}
		if rawConn, err := c.SyscallConn(); err == nil {
			rawConn.Control(data.Getsockopt)
			return data
		}
		return nil
	}
	service.SockoptToProto = sockoptToProto
}

// SocketOptionData defines the struct to hold socket option data, and related
// getter function to obtain info from fd.
type socketOptionData struct {
	Linger      *unix.Linger
	RecvTimeout *unix.Timeval
	SendTimeout *unix.Timeval
	TCPInfo     *unix.TCPInfo
}

// Getsockopt defines the function to get socket options requested by channelz.
// It is to be passed to syscall.RawConn.Control().
func (s *socketOptionData) Getsockopt(fd uintptr) {
	if v, err := unix.GetsockoptLinger(int(fd), syscall.SOL_SOCKET, syscall.SO_LINGER); err == nil {
		s.Linger = v
	}
	if v, err := unix.GetsockoptTimeval(int(fd), syscall.SOL_SOCKET, syscall.SO_RCVTIMEO); err == nil {
		s.RecvTimeout = v
	}
	if v, err := unix.GetsockoptTimeval(int(fd), syscall.SOL_SOCKET, syscall.SO_SNDTIMEO); err == nil {
		s.SendTimeout = v
	}
	if v, err := unix.GetsockoptTCPInfo(int(fd), syscall.SOL_TCP, syscall.TCP_INFO); err == nil {
		s.TCPInfo = v
	}
	return
}

func convertToPtypesDuration(sec int64, usec int64) *durpb.Duration {
	return ptypes.DurationProto(time.Duration(sec*1e9 + usec*1e3))
}

func sockoptToProto(s channelz.SocketOptionData) []*channelzpb.SocketOption {
	var skopts *socketOptionData
	var ok bool
	if skopts, ok = s.(*socketOptionData); !ok {
		return nil
	}
	var opts []*channelzpb.SocketOption
	if skopts.Linger != nil {
		additional, err := ptypes.MarshalAny(&channelzpb.SocketOptionLinger{
			Active:   skopts.Linger.Onoff != 0,
			Duration: convertToPtypesDuration(int64(skopts.Linger.Linger), 0),
		})
		if err == nil {
			opts = append(opts, &channelzpb.SocketOption{
				Name:       "SO_LINGER",
				Additional: additional,
			})
		}
	}
	if skopts.RecvTimeout != nil {
		additional, err := ptypes.MarshalAny(&channelzpb.SocketOptionTimeout{
			Duration: convertToPtypesDuration(int64(skopts.RecvTimeout.Sec), int64(skopts.RecvTimeout.Usec)),
		})
		if err == nil {
			opts = append(opts, &channelzpb.SocketOption{
				Name:       "SO_RCVTIMEO",
				Additional: additional,
			})
		}
	}
	if skopts.SendTimeout != nil {
		additional, err := ptypes.MarshalAny(&channelzpb.SocketOptionTimeout{
			Duration: convertToPtypesDuration(int64(skopts.SendTimeout.Sec), int64(skopts.SendTimeout.Usec)),
		})
		if err == nil {
			opts = append(opts, &channelzpb.SocketOption{
				Name:       "SO_SNDTIMEO",
				Additional: additional,
			})
		}
	}
	if skopts.TCPInfo != nil {
		additional, err := ptypes.MarshalAny(&channelzpb.SocketOptionTcpInfo{
			TcpiState:       uint32(skopts.TCPInfo.State),
			TcpiCaState:     uint32(skopts.TCPInfo.Ca_state),
			TcpiRetransmits: uint32(skopts.TCPInfo.Retransmits),
			TcpiProbes:      uint32(skopts.TCPInfo.Probes),
			TcpiBackoff:     uint32(skopts.TCPInfo.Backoff),
			TcpiOptions:     uint32(skopts.TCPInfo.Options),
			// https://golang.org/pkg/syscall/#TCPInfo
			// TCPInfo struct does not contain info about TcpiSndWscale and TcpiRcvWscale.
			TcpiRto:          skopts.TCPInfo.Rto,
			TcpiAto:          skopts.TCPInfo.Ato,
			TcpiSndMss:       skopts.TCPInfo.Snd_mss,
			TcpiRcvMss:       skopts.TCPInfo.Rcv_mss,
			TcpiUnacked:      skopts.TCPInfo.Unacked,
			TcpiSacked:       skopts.TCPInfo.Sacked,
			TcpiLost:         skopts.TCPInfo.Lost,
			TcpiRetrans:      skopts.TCPInfo.Retrans,
			TcpiFackets:      skopts.TCPInfo.Fackets,
			TcpiLastDataSent: skopts.TCPInfo.Last_data_sent,
			TcpiLastAckSent:  skopts.TCPInfo.Last_ack_sent,
			TcpiLastDataRecv: skopts.TCPInfo.Last_data_recv,
			TcpiLastAckRecv:  skopts.TCPInfo.Last_ack_recv,
			TcpiPmtu:         skopts.TCPInfo.Pmtu,
			TcpiRcvSsthresh:  skopts.TCPInfo.Rcv_ssthresh,
			TcpiRtt:          skopts.TCPInfo.Rtt,
			TcpiRttvar:       skopts.TCPInfo.Rttvar,
			TcpiSndSsthresh:  skopts.TCPInfo.Snd_ssthresh,
			TcpiSndCwnd:      skopts.TCPInfo.Snd_cwnd,
			TcpiAdvmss:       skopts.TCPInfo.Advmss,
			TcpiReordering:   skopts.TCPInfo.Reordering,
		})
		if err == nil {
			opts = append(opts, &channelzpb.SocketOption{
				Name:       "TCP_INFO",
				Additional: additional,
			})
		}
	}
	return opts
}
