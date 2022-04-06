// Copyright 2022 The gRPC Authors
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

// Observability Config is used by gRPC Observability plugin to control provided
// observability features. It contains parameters to enable/disable certain
// features, or fine tune the verbosity.
//
// Note that gRPC may use this config in JSON form, not in protobuf form. This
// proto definition is intended to help document the schema but might not
// actually be used directly by gRPC.

// Code generated by protoc-gen-go. DO NOT EDIT.
// versions:
// 	protoc-gen-go v1.25.0
// 	protoc        v3.14.0
// source: gcp/observability/internal/config/config.proto

package config

import (
	proto "github.com/golang/protobuf/proto"
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

// This is a compile-time assertion that a sufficiently up-to-date version
// of the legacy proto package is being used.
const _ = proto.ProtoPackageIsVersion4

// Configuration for observability behaviors. By default, no configuration is
// required for tracing/metrics/logging to function. This config captures the
// most common knobs for gRPC users. It's always possible to override with
// explicit config in code.
type ObservabilityConfig struct {
	state         protoimpl.MessageState
	sizeCache     protoimpl.SizeCache
	unknownFields protoimpl.UnknownFields

	// Whether the logging data uploading to CloudLogging should be enabled or
	// not. The default value is true.
	EnableCloudLogging bool `protobuf:"varint,1,opt,name=enable_cloud_logging,json=enableCloudLogging,proto3" json:"enable_cloud_logging,omitempty"`
	// The destination GCP project identifier for the uploading log entries. If
	// empty, the gRPC Observability plugin will attempt to fetch the project_id
	// from the GCP environment variables, or from the default credentials.
	DestinationProjectId string `protobuf:"bytes,2,opt,name=destination_project_id,json=destinationProjectId,proto3" json:"destination_project_id,omitempty"`
	// A list of method config. The order matters here - the first pattern which
	// matches the current method will apply the associated config options in
	// the LogFilter. Any other LogFilter that also matches that comes later
	// will be ignored. So a LogFilter of "*/*" should appear last in this list.
	LogFilters []*ObservabilityConfig_LogFilter `protobuf:"bytes,3,rep,name=log_filters,json=logFilters,proto3" json:"log_filters,omitempty"`
}

func (x *ObservabilityConfig) Reset() {
	*x = ObservabilityConfig{}
	if protoimpl.UnsafeEnabled {
		mi := &file_gcp_observability_internal_config_config_proto_msgTypes[0]
		ms := protoimpl.X.MessageStateOf(protoimpl.Pointer(x))
		ms.StoreMessageInfo(mi)
	}
}

func (x *ObservabilityConfig) String() string {
	return protoimpl.X.MessageStringOf(x)
}

func (*ObservabilityConfig) ProtoMessage() {}

func (x *ObservabilityConfig) ProtoReflect() protoreflect.Message {
	mi := &file_gcp_observability_internal_config_config_proto_msgTypes[0]
	if protoimpl.UnsafeEnabled && x != nil {
		ms := protoimpl.X.MessageStateOf(protoimpl.Pointer(x))
		if ms.LoadMessageInfo() == nil {
			ms.StoreMessageInfo(mi)
		}
		return ms
	}
	return mi.MessageOf(x)
}

// Deprecated: Use ObservabilityConfig.ProtoReflect.Descriptor instead.
func (*ObservabilityConfig) Descriptor() ([]byte, []int) {
	return file_gcp_observability_internal_config_config_proto_rawDescGZIP(), []int{0}
}

func (x *ObservabilityConfig) GetEnableCloudLogging() bool {
	if x != nil {
		return x.EnableCloudLogging
	}
	return false
}

func (x *ObservabilityConfig) GetDestinationProjectId() string {
	if x != nil {
		return x.DestinationProjectId
	}
	return ""
}

func (x *ObservabilityConfig) GetLogFilters() []*ObservabilityConfig_LogFilter {
	if x != nil {
		return x.LogFilters
	}
	return nil
}

type ObservabilityConfig_LogFilter struct {
	state         protoimpl.MessageState
	sizeCache     protoimpl.SizeCache
	unknownFields protoimpl.UnknownFields

	// Pattern is a string which can select a group of method names. By
	// default, the pattern is an empty string, matching no methods.
	//
	// Only "*" Wildcard is accepted for pattern. A pattern is in the form
	// of <service>/<method> or just a character "*" .
	//
	// If the pattern is "*", it specifies the defaults for all the
	// services; If the pattern is <service>/*, it specifies the defaults
	// for all methods in the specified service <service>; If the pattern is
	// */<method>, this is not supported.
	//
	// Examples:
	//  - "Foo/Bar" selects only the method "Bar" from service "Foo"
	//  - "Foo/*" selects all methods from service "Foo"
	//  - "*" selects all methods from all services.
	Pattern string `protobuf:"bytes,1,opt,name=pattern,proto3" json:"pattern,omitempty"`
	// Number of bytes of each header to log. If the size of the header is
	// greater than the defined limit, content pass the limit will be
	// truncated. The default value is 0.
	HeaderBytes int32 `protobuf:"varint,2,opt,name=header_bytes,json=headerBytes,proto3" json:"header_bytes,omitempty"`
	// Number of bytes of each message to log. If the size of the message is
	// greater than the defined limit, content pass the limit will be
	// truncated. The default value is 0.
	MessageBytes int32 `protobuf:"varint,3,opt,name=message_bytes,json=messageBytes,proto3" json:"message_bytes,omitempty"`
}

func (x *ObservabilityConfig_LogFilter) Reset() {
	*x = ObservabilityConfig_LogFilter{}
	if protoimpl.UnsafeEnabled {
		mi := &file_gcp_observability_internal_config_config_proto_msgTypes[1]
		ms := protoimpl.X.MessageStateOf(protoimpl.Pointer(x))
		ms.StoreMessageInfo(mi)
	}
}

func (x *ObservabilityConfig_LogFilter) String() string {
	return protoimpl.X.MessageStringOf(x)
}

func (*ObservabilityConfig_LogFilter) ProtoMessage() {}

func (x *ObservabilityConfig_LogFilter) ProtoReflect() protoreflect.Message {
	mi := &file_gcp_observability_internal_config_config_proto_msgTypes[1]
	if protoimpl.UnsafeEnabled && x != nil {
		ms := protoimpl.X.MessageStateOf(protoimpl.Pointer(x))
		if ms.LoadMessageInfo() == nil {
			ms.StoreMessageInfo(mi)
		}
		return ms
	}
	return mi.MessageOf(x)
}

// Deprecated: Use ObservabilityConfig_LogFilter.ProtoReflect.Descriptor instead.
func (*ObservabilityConfig_LogFilter) Descriptor() ([]byte, []int) {
	return file_gcp_observability_internal_config_config_proto_rawDescGZIP(), []int{0, 0}
}

func (x *ObservabilityConfig_LogFilter) GetPattern() string {
	if x != nil {
		return x.Pattern
	}
	return ""
}

func (x *ObservabilityConfig_LogFilter) GetHeaderBytes() int32 {
	if x != nil {
		return x.HeaderBytes
	}
	return 0
}

func (x *ObservabilityConfig_LogFilter) GetMessageBytes() int32 {
	if x != nil {
		return x.MessageBytes
	}
	return 0
}

var File_gcp_observability_internal_config_config_proto protoreflect.FileDescriptor

var file_gcp_observability_internal_config_config_proto_rawDesc = []byte{
	0x0a, 0x2e, 0x67, 0x63, 0x70, 0x2f, 0x6f, 0x62, 0x73, 0x65, 0x72, 0x76, 0x61, 0x62, 0x69, 0x6c,
	0x69, 0x74, 0x79, 0x2f, 0x69, 0x6e, 0x74, 0x65, 0x72, 0x6e, 0x61, 0x6c, 0x2f, 0x63, 0x6f, 0x6e,
	0x66, 0x69, 0x67, 0x2f, 0x63, 0x6f, 0x6e, 0x66, 0x69, 0x67, 0x2e, 0x70, 0x72, 0x6f, 0x74, 0x6f,
	0x12, 0x21, 0x67, 0x72, 0x70, 0x63, 0x2e, 0x6f, 0x62, 0x73, 0x65, 0x72, 0x76, 0x61, 0x62, 0x69,
	0x6c, 0x69, 0x74, 0x79, 0x2e, 0x63, 0x6f, 0x6e, 0x66, 0x69, 0x67, 0x2e, 0x76, 0x31, 0x61, 0x6c,
	0x70, 0x68, 0x61, 0x22, 0xcf, 0x02, 0x0a, 0x13, 0x4f, 0x62, 0x73, 0x65, 0x72, 0x76, 0x61, 0x62,
	0x69, 0x6c, 0x69, 0x74, 0x79, 0x43, 0x6f, 0x6e, 0x66, 0x69, 0x67, 0x12, 0x30, 0x0a, 0x14, 0x65,
	0x6e, 0x61, 0x62, 0x6c, 0x65, 0x5f, 0x63, 0x6c, 0x6f, 0x75, 0x64, 0x5f, 0x6c, 0x6f, 0x67, 0x67,
	0x69, 0x6e, 0x67, 0x18, 0x01, 0x20, 0x01, 0x28, 0x08, 0x52, 0x12, 0x65, 0x6e, 0x61, 0x62, 0x6c,
	0x65, 0x43, 0x6c, 0x6f, 0x75, 0x64, 0x4c, 0x6f, 0x67, 0x67, 0x69, 0x6e, 0x67, 0x12, 0x34, 0x0a,
	0x16, 0x64, 0x65, 0x73, 0x74, 0x69, 0x6e, 0x61, 0x74, 0x69, 0x6f, 0x6e, 0x5f, 0x70, 0x72, 0x6f,
	0x6a, 0x65, 0x63, 0x74, 0x5f, 0x69, 0x64, 0x18, 0x02, 0x20, 0x01, 0x28, 0x09, 0x52, 0x14, 0x64,
	0x65, 0x73, 0x74, 0x69, 0x6e, 0x61, 0x74, 0x69, 0x6f, 0x6e, 0x50, 0x72, 0x6f, 0x6a, 0x65, 0x63,
	0x74, 0x49, 0x64, 0x12, 0x61, 0x0a, 0x0b, 0x6c, 0x6f, 0x67, 0x5f, 0x66, 0x69, 0x6c, 0x74, 0x65,
	0x72, 0x73, 0x18, 0x03, 0x20, 0x03, 0x28, 0x0b, 0x32, 0x40, 0x2e, 0x67, 0x72, 0x70, 0x63, 0x2e,
	0x6f, 0x62, 0x73, 0x65, 0x72, 0x76, 0x61, 0x62, 0x69, 0x6c, 0x69, 0x74, 0x79, 0x2e, 0x63, 0x6f,
	0x6e, 0x66, 0x69, 0x67, 0x2e, 0x76, 0x31, 0x61, 0x6c, 0x70, 0x68, 0x61, 0x2e, 0x4f, 0x62, 0x73,
	0x65, 0x72, 0x76, 0x61, 0x62, 0x69, 0x6c, 0x69, 0x74, 0x79, 0x43, 0x6f, 0x6e, 0x66, 0x69, 0x67,
	0x2e, 0x4c, 0x6f, 0x67, 0x46, 0x69, 0x6c, 0x74, 0x65, 0x72, 0x52, 0x0a, 0x6c, 0x6f, 0x67, 0x46,
	0x69, 0x6c, 0x74, 0x65, 0x72, 0x73, 0x1a, 0x6d, 0x0a, 0x09, 0x4c, 0x6f, 0x67, 0x46, 0x69, 0x6c,
	0x74, 0x65, 0x72, 0x12, 0x18, 0x0a, 0x07, 0x70, 0x61, 0x74, 0x74, 0x65, 0x72, 0x6e, 0x18, 0x01,
	0x20, 0x01, 0x28, 0x09, 0x52, 0x07, 0x70, 0x61, 0x74, 0x74, 0x65, 0x72, 0x6e, 0x12, 0x21, 0x0a,
	0x0c, 0x68, 0x65, 0x61, 0x64, 0x65, 0x72, 0x5f, 0x62, 0x79, 0x74, 0x65, 0x73, 0x18, 0x02, 0x20,
	0x01, 0x28, 0x05, 0x52, 0x0b, 0x68, 0x65, 0x61, 0x64, 0x65, 0x72, 0x42, 0x79, 0x74, 0x65, 0x73,
	0x12, 0x23, 0x0a, 0x0d, 0x6d, 0x65, 0x73, 0x73, 0x61, 0x67, 0x65, 0x5f, 0x62, 0x79, 0x74, 0x65,
	0x73, 0x18, 0x03, 0x20, 0x01, 0x28, 0x05, 0x52, 0x0c, 0x6d, 0x65, 0x73, 0x73, 0x61, 0x67, 0x65,
	0x42, 0x79, 0x74, 0x65, 0x73, 0x42, 0x74, 0x0a, 0x1c, 0x69, 0x6f, 0x2e, 0x67, 0x72, 0x70, 0x63,
	0x2e, 0x6f, 0x62, 0x73, 0x65, 0x72, 0x76, 0x61, 0x62, 0x69, 0x6c, 0x69, 0x74, 0x79, 0x2e, 0x63,
	0x6f, 0x6e, 0x66, 0x69, 0x67, 0x42, 0x18, 0x4f, 0x62, 0x73, 0x65, 0x72, 0x76, 0x61, 0x62, 0x69,
	0x6c, 0x69, 0x74, 0x79, 0x43, 0x6f, 0x6e, 0x66, 0x69, 0x67, 0x50, 0x72, 0x6f, 0x74, 0x6f, 0x50,
	0x01, 0x5a, 0x38, 0x67, 0x6f, 0x6f, 0x67, 0x6c, 0x65, 0x2e, 0x67, 0x6f, 0x6c, 0x61, 0x6e, 0x67,
	0x2e, 0x6f, 0x72, 0x67, 0x2f, 0x67, 0x72, 0x70, 0x63, 0x2f, 0x67, 0x63, 0x70, 0x2f, 0x6f, 0x62,
	0x73, 0x65, 0x72, 0x76, 0x61, 0x62, 0x69, 0x6c, 0x69, 0x74, 0x79, 0x2f, 0x69, 0x6e, 0x74, 0x65,
	0x72, 0x6e, 0x61, 0x6c, 0x2f, 0x63, 0x6f, 0x6e, 0x66, 0x69, 0x67, 0x62, 0x06, 0x70, 0x72, 0x6f,
	0x74, 0x6f, 0x33,
}

var (
	file_gcp_observability_internal_config_config_proto_rawDescOnce sync.Once
	file_gcp_observability_internal_config_config_proto_rawDescData = file_gcp_observability_internal_config_config_proto_rawDesc
)

func file_gcp_observability_internal_config_config_proto_rawDescGZIP() []byte {
	file_gcp_observability_internal_config_config_proto_rawDescOnce.Do(func() {
		file_gcp_observability_internal_config_config_proto_rawDescData = protoimpl.X.CompressGZIP(file_gcp_observability_internal_config_config_proto_rawDescData)
	})
	return file_gcp_observability_internal_config_config_proto_rawDescData
}

var file_gcp_observability_internal_config_config_proto_msgTypes = make([]protoimpl.MessageInfo, 2)
var file_gcp_observability_internal_config_config_proto_goTypes = []interface{}{
	(*ObservabilityConfig)(nil),           // 0: grpc.observability.config.v1alpha.ObservabilityConfig
	(*ObservabilityConfig_LogFilter)(nil), // 1: grpc.observability.config.v1alpha.ObservabilityConfig.LogFilter
}
var file_gcp_observability_internal_config_config_proto_depIdxs = []int32{
	1, // 0: grpc.observability.config.v1alpha.ObservabilityConfig.log_filters:type_name -> grpc.observability.config.v1alpha.ObservabilityConfig.LogFilter
	1, // [1:1] is the sub-list for method output_type
	1, // [1:1] is the sub-list for method input_type
	1, // [1:1] is the sub-list for extension type_name
	1, // [1:1] is the sub-list for extension extendee
	0, // [0:1] is the sub-list for field type_name
}

func init() { file_gcp_observability_internal_config_config_proto_init() }
func file_gcp_observability_internal_config_config_proto_init() {
	if File_gcp_observability_internal_config_config_proto != nil {
		return
	}
	if !protoimpl.UnsafeEnabled {
		file_gcp_observability_internal_config_config_proto_msgTypes[0].Exporter = func(v interface{}, i int) interface{} {
			switch v := v.(*ObservabilityConfig); i {
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
		file_gcp_observability_internal_config_config_proto_msgTypes[1].Exporter = func(v interface{}, i int) interface{} {
			switch v := v.(*ObservabilityConfig_LogFilter); i {
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
			RawDescriptor: file_gcp_observability_internal_config_config_proto_rawDesc,
			NumEnums:      0,
			NumMessages:   2,
			NumExtensions: 0,
			NumServices:   0,
		},
		GoTypes:           file_gcp_observability_internal_config_config_proto_goTypes,
		DependencyIndexes: file_gcp_observability_internal_config_config_proto_depIdxs,
		MessageInfos:      file_gcp_observability_internal_config_config_proto_msgTypes,
	}.Build()
	File_gcp_observability_internal_config_config_proto = out.File
	file_gcp_observability_internal_config_config_proto_rawDesc = nil
	file_gcp_observability_internal_config_config_proto_goTypes = nil
	file_gcp_observability_internal_config_config_proto_depIdxs = nil
}
