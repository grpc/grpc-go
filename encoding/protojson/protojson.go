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

// Package protojson defines the protojson codec. Importing this package will
// register the codec.
package protojson

import (
	"fmt"

	"google.golang.org/protobuf/proto"

	"google.golang.org/grpc/encoding"
	"google.golang.org/protobuf/encoding/protojson"
)

// Name is the name registered for the proto compressor.
const Name = "protojson"

func init() {
	encoding.RegisterCodec(codec{})
}

// RegisterProtoJSONCodec Customization of encoding and decoding can be achieved through methods
func RegisterProtoJSONCodec(maOption protojson.MarshalOptions, unMaOption protojson.UnmarshalOptions) {
	encoding.RegisterCodec(codec{ma: maOption, unMa: unMaOption})
}

// codec is a Codec implementation with protojson. It is a option choice for gRPC.
type codec struct {
	ma   protojson.MarshalOptions
	unMa protojson.UnmarshalOptions
}

func (c codec) Name() string {
	return Name
}
func (c codec) Marshal(v any) ([]byte, error) {
	vv, ok := v.(proto.Message)
	if !ok {
		return nil, fmt.Errorf("failed to marshal, message is %T, want proto.Message", v)
	}
	return c.ma.Marshal(vv)

}

func (c codec) Unmarshal(data []byte, v any) error {
	vv, ok := v.(proto.Message)
	if !ok {
		return fmt.Errorf("failed to unmarshal, message is %T, want proto.Message", v)
	}
	return c.unMa.Unmarshal(data, vv)
}
