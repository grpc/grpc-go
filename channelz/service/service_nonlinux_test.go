// +build !linux appengine

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

// Non-linux system does not support socket option. Therefore, the function
// socketProtoToStruct defined in this file skips the parsing of socket option field.

package service

import (
	"github.com/golang/protobuf/ptypes"
	channelzpb "google.golang.org/grpc/channelz/grpc_channelz_v1"
)

func socketProtoToStruct(s *channelzpb.Socket) *dummySocket {
	ds := &dummySocket{}
	pdata := s.GetData()
	ds.streamsStarted = pdata.GetStreamsStarted()
	ds.streamsSucceeded = pdata.GetStreamsSucceeded()
	ds.streamsFailed = pdata.GetStreamsFailed()
	ds.messagesSent = pdata.GetMessagesSent()
	ds.messagesReceived = pdata.GetMessagesReceived()
	ds.keepAlivesSent = pdata.GetKeepAlivesSent()
	if t, err := ptypes.Timestamp(pdata.GetLastLocalStreamCreatedTimestamp()); err == nil {
		if !t.Equal(emptyTime) {
			ds.lastLocalStreamCreatedTimestamp = t
		}
	}
	if t, err := ptypes.Timestamp(pdata.GetLastRemoteStreamCreatedTimestamp()); err == nil {
		if !t.Equal(emptyTime) {
			ds.lastRemoteStreamCreatedTimestamp = t
		}
	}
	if t, err := ptypes.Timestamp(pdata.GetLastMessageSentTimestamp()); err == nil {
		if !t.Equal(emptyTime) {
			ds.lastMessageSentTimestamp = t
		}
	}
	if t, err := ptypes.Timestamp(pdata.GetLastMessageReceivedTimestamp()); err == nil {
		if !t.Equal(emptyTime) {
			ds.lastMessageReceivedTimestamp = t
		}
	}
	if v := pdata.GetLocalFlowControlWindow(); v != nil {
		ds.localFlowControlWindow = v.Value
	}
	if v := pdata.GetRemoteFlowControlWindow(); v != nil {
		ds.remoteFlowControlWindow = v.Value
	}
	if v := s.GetSecurity(); v != nil {
		ds.security = protoToSecurity(v)
	}
	if local := s.GetLocal(); local != nil {
		ds.localAddr = protoToAddr(local)
	}
	if remote := s.GetRemote(); remote != nil {
		ds.remoteAddr = protoToAddr(remote)
	}
	ds.remoteName = s.GetRemoteName()
	return ds
}
