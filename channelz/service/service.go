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

// Package service provides an implementation for channelz service server.
package service

import (
	"context"
	"net"
	"time"

	"github.com/golang/protobuf/ptypes"
	wrpb "github.com/golang/protobuf/ptypes/wrappers"
	channelzgrpc "google.golang.org/grpc/channelz/grpc_channelz_v1"
	channelzpb "google.golang.org/grpc/channelz/grpc_channelz_v1"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/connectivity"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/grpclog"
	"google.golang.org/grpc/internal/channelz"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/protoadapt"
	"google.golang.org/protobuf/types/known/anypb"
)

func init() {
	channelz.TurnOn()
}

var logger = grpclog.Component("channelz")

// RegisterChannelzServiceToServer registers the channelz service to the given server.
//
// Note: it is preferred to use the admin API
// (https://pkg.go.dev/google.golang.org/grpc/admin#Register) instead to
// register Channelz and other administrative services.
func RegisterChannelzServiceToServer(s grpc.ServiceRegistrar) {
	channelzgrpc.RegisterChannelzServer(s, newCZServer())
}

func newCZServer() channelzgrpc.ChannelzServer {
	return &serverImpl{}
}

type serverImpl struct {
	channelzgrpc.UnimplementedChannelzServer
}

func connectivityStateToProto(s *connectivity.State) *channelzpb.ChannelConnectivityState {
	if s == nil {
		return &channelzpb.ChannelConnectivityState{State: channelzpb.ChannelConnectivityState_UNKNOWN}
	}
	switch *s {
	case connectivity.Idle:
		return &channelzpb.ChannelConnectivityState{State: channelzpb.ChannelConnectivityState_IDLE}
	case connectivity.Connecting:
		return &channelzpb.ChannelConnectivityState{State: channelzpb.ChannelConnectivityState_CONNECTING}
	case connectivity.Ready:
		return &channelzpb.ChannelConnectivityState{State: channelzpb.ChannelConnectivityState_READY}
	case connectivity.TransientFailure:
		return &channelzpb.ChannelConnectivityState{State: channelzpb.ChannelConnectivityState_TRANSIENT_FAILURE}
	case connectivity.Shutdown:
		return &channelzpb.ChannelConnectivityState{State: channelzpb.ChannelConnectivityState_SHUTDOWN}
	default:
		return &channelzpb.ChannelConnectivityState{State: channelzpb.ChannelConnectivityState_UNKNOWN}
	}
}

func channelTraceToProto(ct *channelz.ChannelTrace) *channelzpb.ChannelTrace {
	pbt := &channelzpb.ChannelTrace{}
	if ct == nil {
		return pbt
	}
	pbt.NumEventsLogged = ct.EventNum
	if ts, err := ptypes.TimestampProto(ct.CreationTime); err == nil {
		pbt.CreationTimestamp = ts
	}
	events := make([]*channelzpb.ChannelTraceEvent, 0, len(ct.Events))
	for _, e := range ct.Events {
		cte := &channelzpb.ChannelTraceEvent{
			Description: e.Desc,
			Severity:    channelzpb.ChannelTraceEvent_Severity(e.Severity),
		}
		if ts, err := ptypes.TimestampProto(e.Timestamp); err == nil {
			cte.Timestamp = ts
		}
		if e.RefID != 0 {
			switch e.RefType {
			case channelz.RefChannel:
				cte.ChildRef = &channelzpb.ChannelTraceEvent_ChannelRef{ChannelRef: &channelzpb.ChannelRef{ChannelId: e.RefID, Name: e.RefName}}
			case channelz.RefSubChannel:
				cte.ChildRef = &channelzpb.ChannelTraceEvent_SubchannelRef{SubchannelRef: &channelzpb.SubchannelRef{SubchannelId: e.RefID, Name: e.RefName}}
			}
		}
		events = append(events, cte)
	}
	pbt.Events = events
	return pbt
}

func strFromPointer(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}

func channelMetricToProto(cm *channelz.Channel) *channelzpb.Channel {
	c := &channelzpb.Channel{}
	c.Ref = &channelzpb.ChannelRef{ChannelId: cm.ID, Name: cm.RefName}

	c.Data = &channelzpb.ChannelData{
		State:          connectivityStateToProto(cm.ChannelMetrics.State.Load()),
		Target:         strFromPointer(cm.ChannelMetrics.Target.Load()),
		CallsStarted:   cm.ChannelMetrics.CallsStarted.Load(),
		CallsSucceeded: cm.ChannelMetrics.CallsSucceeded.Load(),
		CallsFailed:    cm.ChannelMetrics.CallsFailed.Load(),
	}
	if ts, err := ptypes.TimestampProto(time.Unix(0, cm.ChannelMetrics.LastCallStartedTimestamp.Load())); err == nil {
		c.Data.LastCallStartedTimestamp = ts
	}
	ncs := cm.NestedChans()
	nestedChans := make([]*channelzpb.ChannelRef, 0, len(ncs))
	for id, ref := range ncs {
		nestedChans = append(nestedChans, &channelzpb.ChannelRef{ChannelId: id, Name: ref})
	}
	c.ChannelRef = nestedChans

	scs := cm.SubChans()
	subChans := make([]*channelzpb.SubchannelRef, 0, len(scs))
	for id, ref := range scs {
		subChans = append(subChans, &channelzpb.SubchannelRef{SubchannelId: id, Name: ref})
	}
	c.SubchannelRef = subChans

	c.Data.Trace = channelTraceToProto(cm.Trace())
	return c
}

func subChannelMetricToProto(cm *channelz.SubChannel) *channelzpb.Subchannel {
	sc := &channelzpb.Subchannel{}
	sc.Ref = &channelzpb.SubchannelRef{SubchannelId: cm.ID, Name: cm.RefName}

	sc.Data = &channelzpb.ChannelData{
		State:          connectivityStateToProto(cm.ChannelMetrics.State.Load()),
		Target:         strFromPointer(cm.ChannelMetrics.Target.Load()),
		CallsStarted:   cm.ChannelMetrics.CallsStarted.Load(),
		CallsSucceeded: cm.ChannelMetrics.CallsSucceeded.Load(),
		CallsFailed:    cm.ChannelMetrics.CallsFailed.Load(),
	}
	if ts, err := ptypes.TimestampProto(time.Unix(0, cm.ChannelMetrics.LastCallStartedTimestamp.Load())); err == nil {
		sc.Data.LastCallStartedTimestamp = ts
	}

	skts := cm.Sockets()
	sockets := make([]*channelzpb.SocketRef, 0, len(skts))
	for id, ref := range skts {
		sockets = append(sockets, &channelzpb.SocketRef{SocketId: id, Name: ref})
	}
	sc.SocketRef = sockets
	sc.Data.Trace = channelTraceToProto(cm.Trace())
	return sc
}

func securityToProto(se credentials.ChannelzSecurityValue) *channelzpb.Security {
	switch v := se.(type) {
	case *credentials.TLSChannelzSecurityValue:
		return &channelzpb.Security{Model: &channelzpb.Security_Tls_{Tls: &channelzpb.Security_Tls{
			CipherSuite:       &channelzpb.Security_Tls_StandardName{StandardName: v.StandardName},
			LocalCertificate:  v.LocalCertificate,
			RemoteCertificate: v.RemoteCertificate,
		}}}
	case *credentials.OtherChannelzSecurityValue:
		otherSecurity := &channelzpb.Security_OtherSecurity{
			Name: v.Name,
		}
		if anyval, err := anypb.New(protoadapt.MessageV2Of(v.Value)); err == nil {
			otherSecurity.Value = anyval
		}
		return &channelzpb.Security{Model: &channelzpb.Security_Other{Other: otherSecurity}}
	}
	return nil
}

func addrToProto(a net.Addr) *channelzpb.Address {
	if a == nil {
		return nil
	}
	switch a.Network() {
	case "udp":
		// TODO: Address_OtherAddress{}. Need proto def for Value.
	case "ip":
		// Note zone info is discarded through the conversion.
		return &channelzpb.Address{Address: &channelzpb.Address_TcpipAddress{TcpipAddress: &channelzpb.Address_TcpIpAddress{IpAddress: a.(*net.IPAddr).IP}}}
	case "ip+net":
		// Note mask info is discarded through the conversion.
		return &channelzpb.Address{Address: &channelzpb.Address_TcpipAddress{TcpipAddress: &channelzpb.Address_TcpIpAddress{IpAddress: a.(*net.IPNet).IP}}}
	case "tcp":
		// Note zone info is discarded through the conversion.
		return &channelzpb.Address{Address: &channelzpb.Address_TcpipAddress{TcpipAddress: &channelzpb.Address_TcpIpAddress{IpAddress: a.(*net.TCPAddr).IP, Port: int32(a.(*net.TCPAddr).Port)}}}
	case "unix", "unixgram", "unixpacket":
		return &channelzpb.Address{Address: &channelzpb.Address_UdsAddress_{UdsAddress: &channelzpb.Address_UdsAddress{Filename: a.String()}}}
	default:
	}
	return &channelzpb.Address{}
}

func socketMetricToProto(skt *channelz.Socket) *channelzpb.Socket {
	s := &channelzpb.Socket{}
	s.Ref = &channelzpb.SocketRef{SocketId: skt.ID, Name: skt.RefName}

	s.Data = &channelzpb.SocketData{
		StreamsStarted:   skt.SocketMetrics.StreamsStarted.Load(),
		StreamsSucceeded: skt.SocketMetrics.StreamsSucceeded.Load(),
		StreamsFailed:    skt.SocketMetrics.StreamsFailed.Load(),
		MessagesSent:     skt.SocketMetrics.MessagesSent.Load(),
		MessagesReceived: skt.SocketMetrics.MessagesReceived.Load(),
		KeepAlivesSent:   skt.SocketMetrics.KeepAlivesSent.Load(),
	}
	if ts, err := ptypes.TimestampProto(time.Unix(0, skt.SocketMetrics.LastLocalStreamCreatedTimestamp.Load())); err == nil {
		s.Data.LastLocalStreamCreatedTimestamp = ts
	}
	if ts, err := ptypes.TimestampProto(time.Unix(0, skt.SocketMetrics.LastRemoteStreamCreatedTimestamp.Load())); err == nil {
		s.Data.LastRemoteStreamCreatedTimestamp = ts
	}
	if ts, err := ptypes.TimestampProto(time.Unix(0, skt.SocketMetrics.LastMessageSentTimestamp.Load())); err == nil {
		s.Data.LastMessageSentTimestamp = ts
	}
	if ts, err := ptypes.TimestampProto(time.Unix(0, skt.SocketMetrics.LastMessageReceivedTimestamp.Load())); err == nil {
		s.Data.LastMessageReceivedTimestamp = ts
	}
	if skt.EphemeralMetrics != nil {
		e := skt.EphemeralMetrics()
		s.Data.LocalFlowControlWindow = &wrpb.Int64Value{Value: e.LocalFlowControlWindow}
		s.Data.RemoteFlowControlWindow = &wrpb.Int64Value{Value: e.RemoteFlowControlWindow}
	}

	s.Data.Option = sockoptToProto(skt.SocketOptions)
	s.Security = securityToProto(skt.Security)
	s.Local = addrToProto(skt.LocalAddr)
	s.Remote = addrToProto(skt.RemoteAddr)
	s.RemoteName = skt.RemoteName
	return s
}

func (s *serverImpl) GetTopChannels(ctx context.Context, req *channelzpb.GetTopChannelsRequest) (*channelzpb.GetTopChannelsResponse, error) {
	chans, end := channelz.GetTopChannels(req.GetStartChannelId(), int(req.GetMaxResults()))
	resp := &channelzpb.GetTopChannelsResponse{}
	for _, ch := range chans {
		resp.Channel = append(resp.Channel, channelMetricToProto(ch))
	}
	resp.End = end
	return resp, nil
}

func serverMetricToProto(sm *channelz.Server) *channelzpb.Server {
	s := &channelzpb.Server{}
	s.Ref = &channelzpb.ServerRef{ServerId: sm.ID, Name: sm.RefName}

	s.Data = &channelzpb.ServerData{
		CallsStarted:   sm.ServerMetrics.CallsStarted.Load(),
		CallsSucceeded: sm.ServerMetrics.CallsSucceeded.Load(),
		CallsFailed:    sm.ServerMetrics.CallsFailed.Load(),
	}

	if ts, err := ptypes.TimestampProto(time.Unix(0, sm.ServerMetrics.LastCallStartedTimestamp.Load())); err == nil {
		s.Data.LastCallStartedTimestamp = ts
	}
	lss := sm.ListenSockets()
	sockets := make([]*channelzpb.SocketRef, 0, len(lss))
	for id, ref := range lss {
		sockets = append(sockets, &channelzpb.SocketRef{SocketId: id, Name: ref})
	}
	s.ListenSocket = sockets
	return s
}

func (s *serverImpl) GetServers(ctx context.Context, req *channelzpb.GetServersRequest) (*channelzpb.GetServersResponse, error) {
	metrics, end := channelz.GetServers(req.GetStartServerId(), int(req.GetMaxResults()))
	resp := &channelzpb.GetServersResponse{}
	for _, m := range metrics {
		resp.Server = append(resp.Server, serverMetricToProto(m))
	}
	resp.End = end
	return resp, nil
}

func (s *serverImpl) GetServerSockets(ctx context.Context, req *channelzpb.GetServerSocketsRequest) (*channelzpb.GetServerSocketsResponse, error) {
	skts, end := channelz.GetServerSockets(req.GetServerId(), req.GetStartSocketId(), int(req.GetMaxResults()))
	resp := &channelzpb.GetServerSocketsResponse{}
	for _, m := range skts {
		resp.SocketRef = append(resp.SocketRef, &channelzpb.SocketRef{SocketId: m.ID, Name: m.RefName})
	}
	resp.End = end
	return resp, nil
}

func (s *serverImpl) GetChannel(ctx context.Context, req *channelzpb.GetChannelRequest) (*channelzpb.GetChannelResponse, error) {
	ch := channelz.GetChannel(req.GetChannelId())
	if ch == nil {
		return nil, status.Errorf(codes.NotFound, "requested channel %d not found", req.GetChannelId())
	}
	resp := &channelzpb.GetChannelResponse{Channel: channelMetricToProto(ch)}
	return resp, nil
}

func (s *serverImpl) GetSubchannel(ctx context.Context, req *channelzpb.GetSubchannelRequest) (*channelzpb.GetSubchannelResponse, error) {
	subChan := channelz.GetSubChannel(req.GetSubchannelId())
	if subChan == nil {
		return nil, status.Errorf(codes.NotFound, "requested sub channel %d not found", req.GetSubchannelId())
	}
	resp := &channelzpb.GetSubchannelResponse{Subchannel: subChannelMetricToProto(subChan)}
	return resp, nil
}

func (s *serverImpl) GetSocket(ctx context.Context, req *channelzpb.GetSocketRequest) (*channelzpb.GetSocketResponse, error) {
	var metric *channelz.Socket
	if metric = channelz.GetSocket(req.GetSocketId()); metric == nil {
		return nil, status.Errorf(codes.NotFound, "requested socket %d not found", req.GetSocketId())
	}
	resp := &channelzpb.GetSocketResponse{Socket: socketMetricToProto(metric)}
	return resp, nil
}

func (s *serverImpl) GetServer(ctx context.Context, req *channelzpb.GetServerRequest) (*channelzpb.GetServerResponse, error) {
	metric := channelz.GetServer(req.GetServerId())
	if metric == nil {
		return nil, status.Errorf(codes.NotFound, "requested server %d not found", req.GetServerId())
	}
	resp := &channelzpb.GetServerResponse{Server: serverMetricToProto(metric)}
	return resp, nil
}
