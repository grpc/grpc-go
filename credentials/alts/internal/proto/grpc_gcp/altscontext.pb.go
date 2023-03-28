// Copyright 2018 The gRPC Authors
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

// The canonical version of this proto can be found at
// https://github.com/grpc/grpc-proto/blob/master/grpc/gcp/altscontext.proto

// Code generated by protoc-gen-go. DO NOT EDIT.
// versions:
// 	protoc-gen-go v1.30.0
// 	protoc        v3.14.0
// source: grpc/gcp/altscontext.proto

package grpc_gcp

import (
	protoreflect "google.golang.org/protobuf/reflect/protoreflect"
	protoimpl "google.golang.org/protobuf/runtime/protoimpl"
	reflect "reflect"
	sync "sync"
)

const (
	// Verify that this generated code is sufficiently up-to-date.
	_ = protoimpl.EnforceVersion(20 - protoimpl.MinVersion)
	// Verify that runtime/protoimpl is sufficiently up-to-date.
	_ = protoimpl.EnforceVersion(protoimpl.MaxVersion - 20)
)

type AltsContext struct {
	state         protoimpl.MessageState
	sizeCache     protoimpl.SizeCache
	unknownFields protoimpl.UnknownFields

	// The application protocol negotiated for this connection.
	ApplicationProtocol string `protobuf:"bytes,1,opt,name=application_protocol,json=applicationProtocol,proto3" json:"application_protocol,omitempty"`
	// The record protocol negotiated for this connection.
	RecordProtocol string `protobuf:"bytes,2,opt,name=record_protocol,json=recordProtocol,proto3" json:"record_protocol,omitempty"`
	// The security level of the created secure channel.
	SecurityLevel SecurityLevel `protobuf:"varint,3,opt,name=security_level,json=securityLevel,proto3,enum=grpc.gcp.SecurityLevel" json:"security_level,omitempty"`
	// The peer service account.
	PeerServiceAccount string `protobuf:"bytes,4,opt,name=peer_service_account,json=peerServiceAccount,proto3" json:"peer_service_account,omitempty"`
	// The local service account.
	LocalServiceAccount string `protobuf:"bytes,5,opt,name=local_service_account,json=localServiceAccount,proto3" json:"local_service_account,omitempty"`
	// The RPC protocol versions supported by the peer.
	PeerRpcVersions *RpcProtocolVersions `protobuf:"bytes,6,opt,name=peer_rpc_versions,json=peerRpcVersions,proto3" json:"peer_rpc_versions,omitempty"`
	// Additional attributes of the peer.
	PeerAttributes map[string]string `protobuf:"bytes,7,rep,name=peer_attributes,json=peerAttributes,proto3" json:"peer_attributes,omitempty" protobuf_key:"bytes,1,opt,name=key,proto3" protobuf_val:"bytes,2,opt,name=value,proto3"`
}

func (x *AltsContext) Reset() {
	*x = AltsContext{}
	if protoimpl.UnsafeEnabled {
		mi := &file_grpc_gcp_altscontext_proto_msgTypes[0]
		ms := protoimpl.X.MessageStateOf(protoimpl.Pointer(x))
		ms.StoreMessageInfo(mi)
	}
}

func (x *AltsContext) String() string {
	return protoimpl.X.MessageStringOf(x)
}

func (*AltsContext) ProtoMessage() {}

func (x *AltsContext) ProtoReflect() protoreflect.Message {
	mi := &file_grpc_gcp_altscontext_proto_msgTypes[0]
	if protoimpl.UnsafeEnabled && x != nil {
		ms := protoimpl.X.MessageStateOf(protoimpl.Pointer(x))
		if ms.LoadMessageInfo() == nil {
			ms.StoreMessageInfo(mi)
		}
		return ms
	}
	return mi.MessageOf(x)
}

// Deprecated: Use AltsContext.ProtoReflect.Descriptor instead.
func (*AltsContext) Descriptor() ([]byte, []int) {
	return file_grpc_gcp_altscontext_proto_rawDescGZIP(), []int{0}
}

func (x *AltsContext) GetApplicationProtocol() string {
	if x != nil {
		return x.ApplicationProtocol
	}
	return ""
}

func (x *AltsContext) GetRecordProtocol() string {
	if x != nil {
		return x.RecordProtocol
	}
	return ""
}

func (x *AltsContext) GetSecurityLevel() SecurityLevel {
	if x != nil {
		return x.SecurityLevel
	}
	return SecurityLevel_SECURITY_NONE
}

func (x *AltsContext) GetPeerServiceAccount() string {
	if x != nil {
		return x.PeerServiceAccount
	}
	return ""
}

func (x *AltsContext) GetLocalServiceAccount() string {
	if x != nil {
		return x.LocalServiceAccount
	}
	return ""
}

func (x *AltsContext) GetPeerRpcVersions() *RpcProtocolVersions {
	if x != nil {
		return x.PeerRpcVersions
	}
	return nil
}

func (x *AltsContext) GetPeerAttributes() map[string]string {
	if x != nil {
		return x.PeerAttributes
	}
	return nil
}

var File_grpc_gcp_altscontext_proto protoreflect.FileDescriptor

var file_grpc_gcp_altscontext_proto_rawDesc = []byte{
	0x0a, 0x1a, 0x67, 0x72, 0x70, 0x63, 0x2f, 0x67, 0x63, 0x70, 0x2f, 0x61, 0x6c, 0x74, 0x73, 0x63,
	0x6f, 0x6e, 0x74, 0x65, 0x78, 0x74, 0x2e, 0x70, 0x72, 0x6f, 0x74, 0x6f, 0x12, 0x08, 0x67, 0x72,
	0x70, 0x63, 0x2e, 0x67, 0x63, 0x70, 0x1a, 0x28, 0x67, 0x72, 0x70, 0x63, 0x2f, 0x67, 0x63, 0x70,
	0x2f, 0x74, 0x72, 0x61, 0x6e, 0x73, 0x70, 0x6f, 0x72, 0x74, 0x5f, 0x73, 0x65, 0x63, 0x75, 0x72,
	0x69, 0x74, 0x79, 0x5f, 0x63, 0x6f, 0x6d, 0x6d, 0x6f, 0x6e, 0x2e, 0x70, 0x72, 0x6f, 0x74, 0x6f,
	0x22, 0xf1, 0x03, 0x0a, 0x0b, 0x41, 0x6c, 0x74, 0x73, 0x43, 0x6f, 0x6e, 0x74, 0x65, 0x78, 0x74,
	0x12, 0x31, 0x0a, 0x14, 0x61, 0x70, 0x70, 0x6c, 0x69, 0x63, 0x61, 0x74, 0x69, 0x6f, 0x6e, 0x5f,
	0x70, 0x72, 0x6f, 0x74, 0x6f, 0x63, 0x6f, 0x6c, 0x18, 0x01, 0x20, 0x01, 0x28, 0x09, 0x52, 0x13,
	0x61, 0x70, 0x70, 0x6c, 0x69, 0x63, 0x61, 0x74, 0x69, 0x6f, 0x6e, 0x50, 0x72, 0x6f, 0x74, 0x6f,
	0x63, 0x6f, 0x6c, 0x12, 0x27, 0x0a, 0x0f, 0x72, 0x65, 0x63, 0x6f, 0x72, 0x64, 0x5f, 0x70, 0x72,
	0x6f, 0x74, 0x6f, 0x63, 0x6f, 0x6c, 0x18, 0x02, 0x20, 0x01, 0x28, 0x09, 0x52, 0x0e, 0x72, 0x65,
	0x63, 0x6f, 0x72, 0x64, 0x50, 0x72, 0x6f, 0x74, 0x6f, 0x63, 0x6f, 0x6c, 0x12, 0x3e, 0x0a, 0x0e,
	0x73, 0x65, 0x63, 0x75, 0x72, 0x69, 0x74, 0x79, 0x5f, 0x6c, 0x65, 0x76, 0x65, 0x6c, 0x18, 0x03,
	0x20, 0x01, 0x28, 0x0e, 0x32, 0x17, 0x2e, 0x67, 0x72, 0x70, 0x63, 0x2e, 0x67, 0x63, 0x70, 0x2e,
	0x53, 0x65, 0x63, 0x75, 0x72, 0x69, 0x74, 0x79, 0x4c, 0x65, 0x76, 0x65, 0x6c, 0x52, 0x0d, 0x73,
	0x65, 0x63, 0x75, 0x72, 0x69, 0x74, 0x79, 0x4c, 0x65, 0x76, 0x65, 0x6c, 0x12, 0x30, 0x0a, 0x14,
	0x70, 0x65, 0x65, 0x72, 0x5f, 0x73, 0x65, 0x72, 0x76, 0x69, 0x63, 0x65, 0x5f, 0x61, 0x63, 0x63,
	0x6f, 0x75, 0x6e, 0x74, 0x18, 0x04, 0x20, 0x01, 0x28, 0x09, 0x52, 0x12, 0x70, 0x65, 0x65, 0x72,
	0x53, 0x65, 0x72, 0x76, 0x69, 0x63, 0x65, 0x41, 0x63, 0x63, 0x6f, 0x75, 0x6e, 0x74, 0x12, 0x32,
	0x0a, 0x15, 0x6c, 0x6f, 0x63, 0x61, 0x6c, 0x5f, 0x73, 0x65, 0x72, 0x76, 0x69, 0x63, 0x65, 0x5f,
	0x61, 0x63, 0x63, 0x6f, 0x75, 0x6e, 0x74, 0x18, 0x05, 0x20, 0x01, 0x28, 0x09, 0x52, 0x13, 0x6c,
	0x6f, 0x63, 0x61, 0x6c, 0x53, 0x65, 0x72, 0x76, 0x69, 0x63, 0x65, 0x41, 0x63, 0x63, 0x6f, 0x75,
	0x6e, 0x74, 0x12, 0x49, 0x0a, 0x11, 0x70, 0x65, 0x65, 0x72, 0x5f, 0x72, 0x70, 0x63, 0x5f, 0x76,
	0x65, 0x72, 0x73, 0x69, 0x6f, 0x6e, 0x73, 0x18, 0x06, 0x20, 0x01, 0x28, 0x0b, 0x32, 0x1d, 0x2e,
	0x67, 0x72, 0x70, 0x63, 0x2e, 0x67, 0x63, 0x70, 0x2e, 0x52, 0x70, 0x63, 0x50, 0x72, 0x6f, 0x74,
	0x6f, 0x63, 0x6f, 0x6c, 0x56, 0x65, 0x72, 0x73, 0x69, 0x6f, 0x6e, 0x73, 0x52, 0x0f, 0x70, 0x65,
	0x65, 0x72, 0x52, 0x70, 0x63, 0x56, 0x65, 0x72, 0x73, 0x69, 0x6f, 0x6e, 0x73, 0x12, 0x52, 0x0a,
	0x0f, 0x70, 0x65, 0x65, 0x72, 0x5f, 0x61, 0x74, 0x74, 0x72, 0x69, 0x62, 0x75, 0x74, 0x65, 0x73,
	0x18, 0x07, 0x20, 0x03, 0x28, 0x0b, 0x32, 0x29, 0x2e, 0x67, 0x72, 0x70, 0x63, 0x2e, 0x67, 0x63,
	0x70, 0x2e, 0x41, 0x6c, 0x74, 0x73, 0x43, 0x6f, 0x6e, 0x74, 0x65, 0x78, 0x74, 0x2e, 0x50, 0x65,
	0x65, 0x72, 0x41, 0x74, 0x74, 0x72, 0x69, 0x62, 0x75, 0x74, 0x65, 0x73, 0x45, 0x6e, 0x74, 0x72,
	0x79, 0x52, 0x0e, 0x70, 0x65, 0x65, 0x72, 0x41, 0x74, 0x74, 0x72, 0x69, 0x62, 0x75, 0x74, 0x65,
	0x73, 0x1a, 0x41, 0x0a, 0x13, 0x50, 0x65, 0x65, 0x72, 0x41, 0x74, 0x74, 0x72, 0x69, 0x62, 0x75,
	0x74, 0x65, 0x73, 0x45, 0x6e, 0x74, 0x72, 0x79, 0x12, 0x10, 0x0a, 0x03, 0x6b, 0x65, 0x79, 0x18,
	0x01, 0x20, 0x01, 0x28, 0x09, 0x52, 0x03, 0x6b, 0x65, 0x79, 0x12, 0x14, 0x0a, 0x05, 0x76, 0x61,
	0x6c, 0x75, 0x65, 0x18, 0x02, 0x20, 0x01, 0x28, 0x09, 0x52, 0x05, 0x76, 0x61, 0x6c, 0x75, 0x65,
	0x3a, 0x02, 0x38, 0x01, 0x42, 0x6c, 0x0a, 0x15, 0x69, 0x6f, 0x2e, 0x67, 0x72, 0x70, 0x63, 0x2e,
	0x61, 0x6c, 0x74, 0x73, 0x2e, 0x69, 0x6e, 0x74, 0x65, 0x72, 0x6e, 0x61, 0x6c, 0x42, 0x10, 0x41,
	0x6c, 0x74, 0x73, 0x43, 0x6f, 0x6e, 0x74, 0x65, 0x78, 0x74, 0x50, 0x72, 0x6f, 0x74, 0x6f, 0x50,
	0x01, 0x5a, 0x3f, 0x67, 0x6f, 0x6f, 0x67, 0x6c, 0x65, 0x2e, 0x67, 0x6f, 0x6c, 0x61, 0x6e, 0x67,
	0x2e, 0x6f, 0x72, 0x67, 0x2f, 0x67, 0x72, 0x70, 0x63, 0x2f, 0x63, 0x72, 0x65, 0x64, 0x65, 0x6e,
	0x74, 0x69, 0x61, 0x6c, 0x73, 0x2f, 0x61, 0x6c, 0x74, 0x73, 0x2f, 0x69, 0x6e, 0x74, 0x65, 0x72,
	0x6e, 0x61, 0x6c, 0x2f, 0x70, 0x72, 0x6f, 0x74, 0x6f, 0x2f, 0x67, 0x72, 0x70, 0x63, 0x5f, 0x67,
	0x63, 0x70, 0x62, 0x06, 0x70, 0x72, 0x6f, 0x74, 0x6f, 0x33,
}

var (
	file_grpc_gcp_altscontext_proto_rawDescOnce sync.Once
	file_grpc_gcp_altscontext_proto_rawDescData = file_grpc_gcp_altscontext_proto_rawDesc
)

func file_grpc_gcp_altscontext_proto_rawDescGZIP() []byte {
	file_grpc_gcp_altscontext_proto_rawDescOnce.Do(func() {
		file_grpc_gcp_altscontext_proto_rawDescData = protoimpl.X.CompressGZIP(file_grpc_gcp_altscontext_proto_rawDescData)
	})
	return file_grpc_gcp_altscontext_proto_rawDescData
}

var file_grpc_gcp_altscontext_proto_msgTypes = make([]protoimpl.MessageInfo, 2)
var file_grpc_gcp_altscontext_proto_goTypes = []interface{}{
	(*AltsContext)(nil),         // 0: grpc.gcp.AltsContext
	nil,                         // 1: grpc.gcp.AltsContext.PeerAttributesEntry
	(SecurityLevel)(0),          // 2: grpc.gcp.SecurityLevel
	(*RpcProtocolVersions)(nil), // 3: grpc.gcp.RpcProtocolVersions
}
var file_grpc_gcp_altscontext_proto_depIdxs = []int32{
	2, // 0: grpc.gcp.AltsContext.security_level:type_name -> grpc.gcp.SecurityLevel
	3, // 1: grpc.gcp.AltsContext.peer_rpc_versions:type_name -> grpc.gcp.RpcProtocolVersions
	1, // 2: grpc.gcp.AltsContext.peer_attributes:type_name -> grpc.gcp.AltsContext.PeerAttributesEntry
	3, // [3:3] is the sub-list for method output_type
	3, // [3:3] is the sub-list for method input_type
	3, // [3:3] is the sub-list for extension type_name
	3, // [3:3] is the sub-list for extension extendee
	0, // [0:3] is the sub-list for field type_name
}

func init() { file_grpc_gcp_altscontext_proto_init() }
func file_grpc_gcp_altscontext_proto_init() {
	if File_grpc_gcp_altscontext_proto != nil {
		return
	}
	file_grpc_gcp_transport_security_common_proto_init()
	if !protoimpl.UnsafeEnabled {
		file_grpc_gcp_altscontext_proto_msgTypes[0].Exporter = func(v interface{}, i int) interface{} {
			switch v := v.(*AltsContext); i {
			case 0:
				return &v.state
			case 1:
				return &v.sizeCache
			case 2:
				return &v.unknownFields
			default:
				return nil
			}
		}
	}
	type x struct{}
	out := protoimpl.TypeBuilder{
		File: protoimpl.DescBuilder{
			GoPackagePath: reflect.TypeOf(x{}).PkgPath(),
			RawDescriptor: file_grpc_gcp_altscontext_proto_rawDesc,
			NumEnums:      0,
			NumMessages:   2,
			NumExtensions: 0,
			NumServices:   0,
		},
		GoTypes:           file_grpc_gcp_altscontext_proto_goTypes,
		DependencyIndexes: file_grpc_gcp_altscontext_proto_depIdxs,
		MessageInfos:      file_grpc_gcp_altscontext_proto_msgTypes,
	}.Build()
	File_grpc_gcp_altscontext_proto = out.File
	file_grpc_gcp_altscontext_proto_rawDesc = nil
	file_grpc_gcp_altscontext_proto_goTypes = nil
	file_grpc_gcp_altscontext_proto_depIdxs = nil
}
