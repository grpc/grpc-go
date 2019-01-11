/*
 *
 * Copyright 2014 gRPC authors.
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

package grpc

import (
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	// "google.golang.org/grpc/encoding"
)

type PreparedMsg struct {
	encodedData 		[]byte 
	compredData 		[]byte
	hdr		 			[]byte
	payload 			[]byte
}

// checks if the rpcInfo has all the correct information
func checkPreparedMsgContext(rpc *rpcInfo) error {
	if rpc.codec == nil {
		return status.Errorf(codes.Internal, "grpc : preparedmsg : rpcInfo.codec is nil")
	}
	if rpc.cp == nil && rpc.comp == nil {
		return status.Errorf(codes.Internal, "grpc : preparedmsg : rpcInfo.cp is nil AND rpcInfo.comp is nil")
	}
	return nil
}

// TODO(prannayk) : if something changes then mark prepared msg as old
// Encode : marshal and compresses data based on stream context
// Returns error in case of error
func (p *PreparedMsg) Encode(s Stream, msg interface{}) error {
	ctx := s.Context()
	rpcInfo, ok := rpcInfoFromContext(ctx)
	if !ok {
		return status.Errorf(codes.Internal, "grpc: PreparedMsg : unable to get rpcInfo")
	}
	err := checkPreparedMsgContext(rpcInfo)
	if err != nil {
		return err
	}
	data, err := encode(rpcInfo.codec, msg)
	if err != nil {
		return err
	}
	p.encodedData = data
	compData, err := compress(data, rpcInfo.cp, rpcInfo.comp)
	if err != nil {
		return err
	}
	p.compredData = compData
	p.hdr, p.payload = msgHeader(data, compData)
	return nil
}
