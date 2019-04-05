// Code generated by protoc-gen-go. DO NOT EDIT.
// source: envoy/api/v2/cluster/outlier_detection.proto

package cluster

import proto "github.com/golang/protobuf/proto"
import fmt "fmt"
import math "math"
import duration "github.com/golang/protobuf/ptypes/duration"
import wrappers "github.com/golang/protobuf/ptypes/wrappers"
import _ "google.golang.org/grpc/balancer/xds/proto/validate"

// Reference imports to suppress errors if they are not otherwise used.
var _ = proto.Marshal
var _ = fmt.Errorf
var _ = math.Inf

// This is a compile-time assertion to ensure that this generated file
// is compatible with the proto package it is being compiled against.
// A compilation error at this line likely means your copy of the
// proto package needs to be updated.
const _ = proto.ProtoPackageIsVersion2 // please upgrade the proto package

// See the :ref:`architecture overview <arch_overview_outlier_detection>` for
// more information on outlier detection.
type OutlierDetection struct {
	// The number of consecutive 5xx responses before a consecutive 5xx ejection
	// occurs. Defaults to 5.
	Consecutive_5Xx *wrappers.UInt32Value `protobuf:"bytes,1,opt,name=consecutive_5xx,json=consecutive5xx,proto3" json:"consecutive_5xx,omitempty"`
	// The time interval between ejection analysis sweeps. This can result in
	// both new ejections as well as hosts being returned to service. Defaults
	// to 10000ms or 10s.
	Interval *duration.Duration `protobuf:"bytes,2,opt,name=interval,proto3" json:"interval,omitempty"`
	// The base time that a host is ejected for. The real time is equal to the
	// base time multiplied by the number of times the host has been ejected.
	// Defaults to 30000ms or 30s.
	BaseEjectionTime *duration.Duration `protobuf:"bytes,3,opt,name=base_ejection_time,json=baseEjectionTime,proto3" json:"base_ejection_time,omitempty"`
	// The maximum % of an upstream cluster that can be ejected due to outlier
	// detection. Defaults to 10% but will eject at least one host regardless of the value.
	MaxEjectionPercent *wrappers.UInt32Value `protobuf:"bytes,4,opt,name=max_ejection_percent,json=maxEjectionPercent,proto3" json:"max_ejection_percent,omitempty"`
	// The % chance that a host will be actually ejected when an outlier status
	// is detected through consecutive 5xx. This setting can be used to disable
	// ejection or to ramp it up slowly. Defaults to 100.
	EnforcingConsecutive_5Xx *wrappers.UInt32Value `protobuf:"bytes,5,opt,name=enforcing_consecutive_5xx,json=enforcingConsecutive5xx,proto3" json:"enforcing_consecutive_5xx,omitempty"`
	// The % chance that a host will be actually ejected when an outlier status
	// is detected through success rate statistics. This setting can be used to
	// disable ejection or to ramp it up slowly. Defaults to 100.
	EnforcingSuccessRate *wrappers.UInt32Value `protobuf:"bytes,6,opt,name=enforcing_success_rate,json=enforcingSuccessRate,proto3" json:"enforcing_success_rate,omitempty"`
	// The number of hosts in a cluster that must have enough request volume to
	// detect success rate outliers. If the number of hosts is less than this
	// setting, outlier detection via success rate statistics is not performed
	// for any host in the cluster. Defaults to 5.
	SuccessRateMinimumHosts *wrappers.UInt32Value `protobuf:"bytes,7,opt,name=success_rate_minimum_hosts,json=successRateMinimumHosts,proto3" json:"success_rate_minimum_hosts,omitempty"`
	// The minimum number of total requests that must be collected in one
	// interval (as defined by the interval duration above) to include this host
	// in success rate based outlier detection. If the volume is lower than this
	// setting, outlier detection via success rate statistics is not performed
	// for that host. Defaults to 100.
	SuccessRateRequestVolume *wrappers.UInt32Value `protobuf:"bytes,8,opt,name=success_rate_request_volume,json=successRateRequestVolume,proto3" json:"success_rate_request_volume,omitempty"`
	// This factor is used to determine the ejection threshold for success rate
	// outlier ejection. The ejection threshold is the difference between the
	// mean success rate, and the product of this factor and the standard
	// deviation of the mean success rate: mean - (stdev *
	// success_rate_stdev_factor). This factor is divided by a thousand to get a
	// double. That is, if the desired factor is 1.9, the runtime value should
	// be 1900. Defaults to 1900.
	SuccessRateStdevFactor *wrappers.UInt32Value `protobuf:"bytes,9,opt,name=success_rate_stdev_factor,json=successRateStdevFactor,proto3" json:"success_rate_stdev_factor,omitempty"`
	// The number of consecutive gateway failures (502, 503, 504 status or
	// connection errors that are mapped to one of those status codes) before a
	// consecutive gateway failure ejection occurs. Defaults to 5.
	ConsecutiveGatewayFailure *wrappers.UInt32Value `protobuf:"bytes,10,opt,name=consecutive_gateway_failure,json=consecutiveGatewayFailure,proto3" json:"consecutive_gateway_failure,omitempty"`
	// The % chance that a host will be actually ejected when an outlier status
	// is detected through consecutive gateway failures. This setting can be
	// used to disable ejection or to ramp it up slowly. Defaults to 0.
	EnforcingConsecutiveGatewayFailure *wrappers.UInt32Value `protobuf:"bytes,11,opt,name=enforcing_consecutive_gateway_failure,json=enforcingConsecutiveGatewayFailure,proto3" json:"enforcing_consecutive_gateway_failure,omitempty"`
	XXX_NoUnkeyedLiteral               struct{}              `json:"-"`
	XXX_unrecognized                   []byte                `json:"-"`
	XXX_sizecache                      int32                 `json:"-"`
}

func (m *OutlierDetection) Reset()         { *m = OutlierDetection{} }
func (m *OutlierDetection) String() string { return proto.CompactTextString(m) }
func (*OutlierDetection) ProtoMessage()    {}
func (*OutlierDetection) Descriptor() ([]byte, []int) {
	return fileDescriptor_outlier_detection_4b620deab17eefc2, []int{0}
}
func (m *OutlierDetection) XXX_Unmarshal(b []byte) error {
	return xxx_messageInfo_OutlierDetection.Unmarshal(m, b)
}
func (m *OutlierDetection) XXX_Marshal(b []byte, deterministic bool) ([]byte, error) {
	return xxx_messageInfo_OutlierDetection.Marshal(b, m, deterministic)
}
func (dst *OutlierDetection) XXX_Merge(src proto.Message) {
	xxx_messageInfo_OutlierDetection.Merge(dst, src)
}
func (m *OutlierDetection) XXX_Size() int {
	return xxx_messageInfo_OutlierDetection.Size(m)
}
func (m *OutlierDetection) XXX_DiscardUnknown() {
	xxx_messageInfo_OutlierDetection.DiscardUnknown(m)
}

var xxx_messageInfo_OutlierDetection proto.InternalMessageInfo

func (m *OutlierDetection) GetConsecutive_5Xx() *wrappers.UInt32Value {
	if m != nil {
		return m.Consecutive_5Xx
	}
	return nil
}

func (m *OutlierDetection) GetInterval() *duration.Duration {
	if m != nil {
		return m.Interval
	}
	return nil
}

func (m *OutlierDetection) GetBaseEjectionTime() *duration.Duration {
	if m != nil {
		return m.BaseEjectionTime
	}
	return nil
}

func (m *OutlierDetection) GetMaxEjectionPercent() *wrappers.UInt32Value {
	if m != nil {
		return m.MaxEjectionPercent
	}
	return nil
}

func (m *OutlierDetection) GetEnforcingConsecutive_5Xx() *wrappers.UInt32Value {
	if m != nil {
		return m.EnforcingConsecutive_5Xx
	}
	return nil
}

func (m *OutlierDetection) GetEnforcingSuccessRate() *wrappers.UInt32Value {
	if m != nil {
		return m.EnforcingSuccessRate
	}
	return nil
}

func (m *OutlierDetection) GetSuccessRateMinimumHosts() *wrappers.UInt32Value {
	if m != nil {
		return m.SuccessRateMinimumHosts
	}
	return nil
}

func (m *OutlierDetection) GetSuccessRateRequestVolume() *wrappers.UInt32Value {
	if m != nil {
		return m.SuccessRateRequestVolume
	}
	return nil
}

func (m *OutlierDetection) GetSuccessRateStdevFactor() *wrappers.UInt32Value {
	if m != nil {
		return m.SuccessRateStdevFactor
	}
	return nil
}

func (m *OutlierDetection) GetConsecutiveGatewayFailure() *wrappers.UInt32Value {
	if m != nil {
		return m.ConsecutiveGatewayFailure
	}
	return nil
}

func (m *OutlierDetection) GetEnforcingConsecutiveGatewayFailure() *wrappers.UInt32Value {
	if m != nil {
		return m.EnforcingConsecutiveGatewayFailure
	}
	return nil
}

func init() {
	proto.RegisterType((*OutlierDetection)(nil), "envoy.api.v2.cluster.OutlierDetection")
}

func init() {
	proto.RegisterFile("envoy/api/v2/cluster/outlier_detection.proto", fileDescriptor_outlier_detection_4b620deab17eefc2)
}

var fileDescriptor_outlier_detection_4b620deab17eefc2 = []byte{
	// 526 bytes of a gzipped FileDescriptorProto
	0x1f, 0x8b, 0x08, 0x00, 0x00, 0x00, 0x00, 0x00, 0x02, 0xff, 0x94, 0x94, 0xed, 0x6a, 0x13, 0x4d,
	0x14, 0xc7, 0x9f, 0xa4, 0xef, 0x53, 0x78, 0x2c, 0x43, 0x4d, 0x26, 0xad, 0x88, 0x04, 0x04, 0x29,
	0x32, 0x0b, 0x29, 0xbd, 0x80, 0xa6, 0x4d, 0xd5, 0x0f, 0x6a, 0x48, 0x34, 0x22, 0x2a, 0xc3, 0x64,
	0x73, 0x12, 0x47, 0x76, 0x77, 0xd6, 0x79, 0xd9, 0x6e, 0xbc, 0xa4, 0x5e, 0x82, 0x9f, 0xbc, 0x1d,
	0xaf, 0xc1, 0x2f, 0xb2, 0x3b, 0x79, 0xd9, 0xa4, 0x01, 0x93, 0x6f, 0x0b, 0x73, 0x7e, 0xbf, 0xff,
	0xd9, 0x99, 0xc3, 0x41, 0xcf, 0x21, 0x4a, 0xe4, 0xd8, 0xe3, 0xb1, 0xf0, 0x92, 0x86, 0xe7, 0x07,
	0x56, 0x1b, 0x50, 0x9e, 0xb4, 0x26, 0x10, 0xa0, 0xd8, 0x00, 0x0c, 0xf8, 0x46, 0xc8, 0x88, 0xc6,
	0x4a, 0x1a, 0x89, 0x8f, 0xf3, 0x6a, 0xca, 0x63, 0x41, 0x93, 0x06, 0x9d, 0x54, 0x9f, 0x3c, 0x1e,
	0x49, 0x39, 0x0a, 0xc0, 0xcb, 0x6b, 0xfa, 0x76, 0xe8, 0x0d, 0xac, 0xe2, 0x73, 0xea, 0xfe, 0xf9,
	0xad, 0xe2, 0x71, 0x0c, 0x4a, 0x4f, 0xce, 0xab, 0x09, 0x0f, 0xc4, 0x80, 0x1b, 0xf0, 0xa6, 0x1f,
	0xee, 0xa0, 0xfe, 0x67, 0x0f, 0x1d, 0xbd, 0x75, 0xad, 0x5c, 0x4f, 0x3b, 0xc1, 0x2d, 0xf4, 0xc0,
	0x97, 0x91, 0x06, 0xdf, 0x1a, 0x91, 0x00, 0xbb, 0x48, 0x53, 0x52, 0x7a, 0x52, 0x7a, 0x76, 0xd8,
	0x78, 0x44, 0x5d, 0x0e, 0x9d, 0xe6, 0xd0, 0xf7, 0xaf, 0x22, 0x73, 0xde, 0xe8, 0xf1, 0xc0, 0x42,
	0xe7, 0xff, 0x02, 0x74, 0x91, 0xa6, 0xf8, 0x12, 0xed, 0x8b, 0xc8, 0x80, 0x4a, 0x78, 0x40, 0xca,
	0x39, 0x5f, 0xbb, 0xc7, 0x5f, 0x4f, 0xfe, 0xa3, 0x89, 0x7e, 0xfe, 0xfe, 0xb5, 0xb5, 0x73, 0x57,
	0x2a, 0x9f, 0xfd, 0xd7, 0x99, 0x61, 0xb8, 0x8b, 0x70, 0x9f, 0x6b, 0x60, 0xf0, 0xcd, 0xb5, 0xc6,
	0x8c, 0x08, 0x81, 0x6c, 0x6d, 0x22, 0x3b, 0xca, 0x04, 0xad, 0x09, 0xff, 0x4e, 0x84, 0x80, 0x3f,
	0xa2, 0xe3, 0x90, 0xa7, 0x73, 0x67, 0x0c, 0xca, 0x87, 0xc8, 0x90, 0xed, 0x7f, 0xff, 0x63, 0xf3,
	0x20, 0x33, 0x6f, 0x9f, 0x95, 0xc9, 0xa0, 0x83, 0x43, 0x9e, 0x4e, 0xbd, 0x6d, 0xa7, 0xc0, 0x3e,
	0xaa, 0x41, 0x34, 0x94, 0xca, 0x17, 0xd1, 0x88, 0x2d, 0xdf, 0xe1, 0xce, 0x66, 0xfe, 0xea, 0xcc,
	0x74, 0xb5, 0x78, 0xaf, 0x5f, 0x50, 0x65, 0x1e, 0xa2, 0xad, 0xef, 0x83, 0xd6, 0x4c, 0x71, 0x03,
	0x64, 0x77, 0xb3, 0x84, 0xe3, 0x99, 0xa6, 0xeb, 0x2c, 0x1d, 0x6e, 0xb2, 0xeb, 0x39, 0x29, 0x4a,
	0x59, 0x28, 0x22, 0x11, 0xda, 0x90, 0x7d, 0x95, 0xda, 0x68, 0xb2, 0xb7, 0xc6, 0x20, 0x54, 0xf5,
	0x5c, 0xf7, 0xda, 0xd1, 0x2f, 0x33, 0x18, 0x7f, 0x42, 0xa7, 0x0b, 0x6a, 0x05, 0xdf, 0x2d, 0x68,
	0xc3, 0x12, 0x19, 0xd8, 0x10, 0xc8, 0xfe, 0x1a, 0x6e, 0x52, 0x70, 0x77, 0x1c, 0xde, 0xcb, 0x69,
	0xfc, 0x01, 0xd5, 0x16, 0xe4, 0xda, 0x0c, 0x20, 0x61, 0x43, 0xee, 0x1b, 0xa9, 0xc8, 0xc1, 0x1a,
	0xea, 0x4a, 0x41, 0xdd, 0xcd, 0xe0, 0x9b, 0x9c, 0xc5, 0x9f, 0xd1, 0x69, 0xf1, 0x29, 0x47, 0xdc,
	0xc0, 0x2d, 0x1f, 0xb3, 0x21, 0x17, 0x81, 0x55, 0x40, 0xd0, 0x1a, 0xea, 0x5a, 0x41, 0xf0, 0xc2,
	0xf1, 0x37, 0x0e, 0xc7, 0x3f, 0xd0, 0xd3, 0xd5, 0x23, 0xb3, 0x9c, 0x73, 0xb8, 0xd9, 0xe3, 0xd6,
	0x57, 0x8d, 0xcf, 0x62, 0x76, 0xb3, 0x87, 0xea, 0x42, 0xd2, 0x7c, 0xe3, 0xc4, 0x4a, 0xa6, 0x63,
	0xba, 0x6a, 0xf9, 0x34, 0x1f, 0x2e, 0x2f, 0x88, 0x76, 0x16, 0xdd, 0x2e, 0xdd, 0x95, 0x2b, 0xad,
	0xbc, 0xfe, 0x32, 0x16, 0xb4, 0xd7, 0xa0, 0x57, 0xae, 0xfe, 0x4d, 0xb7, 0xbf, 0x9b, 0x37, 0x77,
	0xfe, 0x37, 0x00, 0x00, 0xff, 0xff, 0xfa, 0xbc, 0x0f, 0x9b, 0xfb, 0x04, 0x00, 0x00,
}
