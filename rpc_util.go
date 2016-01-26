/*
 *
 * Copyright 2014, Google Inc.
 * All rights reserved.
 *
 * Redistribution and use in source and binary forms, with or without
 * modification, are permitted provided that the following conditions are
 * met:
 *
 *     * Redistributions of source code must retain the above copyright
 * notice, this list of conditions and the following disclaimer.
 *     * Redistributions in binary form must reproduce the above
 * copyright notice, this list of conditions and the following disclaimer
 * in the documentation and/or other materials provided with the
 * distribution.
 *     * Neither the name of Google Inc. nor the names of its
 * contributors may be used to endorse or promote products derived from
 * this software without specific prior written permission.
 *
 * THIS SOFTWARE IS PROVIDED BY THE COPYRIGHT HOLDERS AND CONTRIBUTORS
 * "AS IS" AND ANY EXPRESS OR IMPLIED WARRANTIES, INCLUDING, BUT NOT
 * LIMITED TO, THE IMPLIED WARRANTIES OF MERCHANTABILITY AND FITNESS FOR
 * A PARTICULAR PURPOSE ARE DISCLAIMED. IN NO EVENT SHALL THE COPYRIGHT
 * OWNER OR CONTRIBUTORS BE LIABLE FOR ANY DIRECT, INDIRECT, INCIDENTAL,
 * SPECIAL, EXEMPLARY, OR CONSEQUENTIAL DAMAGES (INCLUDING, BUT NOT
 * LIMITED TO, PROCUREMENT OF SUBSTITUTE GOODS OR SERVICES; LOSS OF USE,
 * DATA, OR PROFITS; OR BUSINESS INTERRUPTION) HOWEVER CAUSED AND ON ANY
 * THEORY OF LIABILITY, WHETHER IN CONTRACT, STRICT LIABILITY, OR TORT
 * (INCLUDING NEGLIGENCE OR OTHERWISE) ARISING IN ANY WAY OUT OF THE USE
 * OF THIS SOFTWARE, EVEN IF ADVISED OF THE POSSIBILITY OF SUCH DAMAGE.
 *
 */

package grpc

import (
	"bytes"
	"compress/gzip"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"io"
	"io/ioutil"
	"math"
	"math/rand"
	"os"
	"time"

	"github.com/golang/protobuf/proto"
	"golang.org/x/net/context"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/transport"
)

const MAX_CODECS = int(16)

//0 for protobuf defalt;1 for json defalt
type CodecType byte

const (
	CODEC_TYPE_PROTO = CodecType(0x00)
	CODEC_TYPE_JSON  = CodecType(0x01)
)

type CompressType byte

const (
	COMPRESS_TYPE_NONE = CompressType(0x00)
	COMPRESS_TYPE_GZIP = CompressType(0x10)
)

// Codec defines the interface gRPC uses to encode and decode messages.
type Codec interface {
	// Marshal returns the wire format of v.
	Marshal(v interface{}) ([]byte, error)
	// Unmarshal parses the wire format into v.
	Unmarshal(data []byte, v interface{}) error
	// String returns the name of the Codec implementation. The returned
	// string will be used as part of content type in transmission.
	String() string

	TypeCode() CodecType
}

var codecs = [MAX_CODECS]Codec{
	NewProtoCodec(),
	NewJsonCodec(),
}

func GetCodec(typ CodecType) (c Codec, err error) {
	index := int(typ)
	if index < 0 || index >= MAX_CODECS {
		return nil, fmt.Errorf("codec type out of range %d", typ)
	}

	if codecs[index] == nil {
		return nil, fmt.Errorf("codec not support %d", typ)
	}

	return codecs[index], nil
}

func RegistCodec(c Codec) (err error) {
	index := int(c.TypeCode())
	if index < 0 || index >= MAX_CODECS {
		return fmt.Errorf("codec type out of range %d", c.TypeCode())
	}

	codecs[index] = c

	return nil
}

// protoCodec is a Codec implemetation with protobuf. It is the default codec for gRPC.
type protoCodec struct{}

func (protoCodec) Marshal(v interface{}) ([]byte, error) {
	return proto.Marshal(v.(proto.Message))
}

func (protoCodec) Unmarshal(data []byte, v interface{}) error {
	return proto.Unmarshal(data, v.(proto.Message))
}

func (protoCodec) String() string {
	return "proto"
}

func (protoCodec) TypeCode() CodecType {
	return CODEC_TYPE_PROTO
}

func NewProtoCodec() (c Codec) {
	return protoCodec{}
}

type jsonCodec struct{}

func (jsonCodec) Marshal(v interface{}) ([]byte, error) {
	return json.Marshal(v)
}

func (jsonCodec) Unmarshal(data []byte, v interface{}) error {
	return json.Unmarshal(data, v)
}

func (jsonCodec) String() string {
	return "json"
}

func (jsonCodec) TypeCode() CodecType {
	return CODEC_TYPE_JSON
}

func NewJsonCodec() (c Codec) {
	return jsonCodec{}
}

// Compressor defines the interface gRPC uses to compress a message.
type Compressor interface {
	// Do compresses p into w.
	Do(w io.Writer, p []byte) error
	// Type returns the compression algorithm the Compressor uses.
	Type() string

	TypeCode() CompressType
}

// NewGZIPCompressor creates a Compressor based on GZIP.
func NewGZIPCompressor() Compressor {
	return &gzipCompressor{}
}

type gzipCompressor struct {
}

func (c *gzipCompressor) Do(w io.Writer, p []byte) error {
	z := gzip.NewWriter(w)
	if _, err := z.Write(p); err != nil {
		return err
	}
	return z.Close()
}

func (c *gzipCompressor) Type() string {
	return "gzip"
}

func (c *gzipCompressor) TypeCode() CompressType {
	return COMPRESS_TYPE_GZIP
}

// Decompressor defines the interface gRPC uses to decompress a message.
type Decompressor interface {
	// Do reads the data from r and uncompress them.
	Do(r io.Reader) ([]byte, error)
	// Type returns the compression algorithm the Decompressor uses.
	Type() string

	TypeCode() CompressType
}

type gzipDecompressor struct {
}

// NewGZIPDecompressor creates a Decompressor based on GZIP.
func NewGZIPDecompressor() Decompressor {
	return &gzipDecompressor{}
}

func (d *gzipDecompressor) Do(r io.Reader) ([]byte, error) {
	z, err := gzip.NewReader(r)
	if err != nil {
		return nil, err
	}
	defer z.Close()
	return ioutil.ReadAll(z)
}

func (d *gzipDecompressor) Type() string {
	return "gzip"
}

func (c *gzipDecompressor) TypeCode() CompressType {
	return COMPRESS_TYPE_GZIP
}

// CompressorGenerator defines the function generating a Compressor.
type CompressorGenerator func() Compressor

// DecompressorGenerator defines the function generating a Decompressor.
type DecompressorGenerator func() Decompressor

// callInfo contains all related configuration and information about an RPC.
type callInfo struct {
	failFast  bool
	headerMD  metadata.MD
	trailerMD metadata.MD
	traceInfo traceInfo // in trace.go
}

// CallOption configures a Call before it starts or extracts information from
// a Call after it completes.
type CallOption interface {
	// before is called before the call is sent to any server.  If before
	// returns a non-nil error, the RPC fails with that error.
	before(*callInfo) error

	// after is called after the call has completed.  after cannot return an
	// error, so any failures should be reported via output parameters.
	after(*callInfo)
}

type beforeCall func(c *callInfo) error

func (o beforeCall) before(c *callInfo) error { return o(c) }
func (o beforeCall) after(c *callInfo)        {}

type afterCall func(c *callInfo)

func (o afterCall) before(c *callInfo) error { return nil }
func (o afterCall) after(c *callInfo)        { o(c) }

// Header returns a CallOptions that retrieves the header metadata
// for a unary RPC.
func Header(md *metadata.MD) CallOption {
	return afterCall(func(c *callInfo) {
		*md = c.headerMD
	})
}

// Trailer returns a CallOptions that retrieves the trailer metadata
// for a unary RPC.
func Trailer(md *metadata.MD) CallOption {
	return afterCall(func(c *callInfo) {
		*md = c.trailerMD
	})
}

// parser reads complelete gRPC messages from the underlying reader.
type parser struct {
	s io.Reader
}

// recvMsg is to read a complete gRPC message from the stream. It is blocking if
// the message has not been complete yet. It returns the message and its type,
// EOF is returned with nil msg and 0 pf if the entire stream is done. Other
// non-nil error is returned if something is wrong on reading.
func (p *parser) recvMsg() (codecType CodecType, compressType CompressType, msg []byte, err error) {
	// The header of a gRPC message. Find more detail
	// at http://www.grpc.io/docs/guides/wire.html.
	var buf [5]byte

	if _, err := io.ReadFull(p.s, buf[:]); err != nil {
		return 0, 0, nil, err
	}

	codecType = CodecType(buf[0] & 0x0F)
	if codecType != CODEC_TYPE_JSON && codecType != CODEC_TYPE_PROTO {
		return 0, 0, nil, fmt.Errorf("codec not support %d", codecType)
	}

	compressType = CompressType(buf[0] & 0xF0)
	if compressType != COMPRESS_TYPE_NONE && compressType != COMPRESS_TYPE_GZIP {
		return 0, 0, nil, fmt.Errorf("compressor not support %d", compressType)
	}

	length := binary.BigEndian.Uint32(buf[1:])
	if length == 0 {
		return codecType, compressType, nil, nil
	}

	msg = make([]byte, int(length))
	if _, err := io.ReadFull(p.s, msg); err != nil {
		if err == io.EOF {
			err = io.ErrUnexpectedEOF
		}
		return 0, 0, nil, err
	}
	return codecType, compressType, msg, nil
}

// encode serializes msg and prepends the message header. If msg is nil, it
// generates the message header of 0 message length.
func encode(c Codec, msg interface{}, cp Compressor, cbuf *bytes.Buffer) ([]byte, error) {
	var b []byte
	var length uint
	if msg != nil {
		var err error
		// TODO(zhaoq): optimize to reduce memory alloc and copying.
		b, err = c.Marshal(msg)
		if err != nil {
			return nil, err
		}
		if cp != nil {
			if err := cp.Do(cbuf, b); err != nil {
				return nil, err
			}
			b = cbuf.Bytes()
		}
		length = uint(len(b))
	}
	if length > math.MaxUint32 {
		return nil, Errorf(codes.InvalidArgument, "grpc: message too large (%d bytes)", length)
	}

	const (
		payloadLen = 1
		sizeLen    = 4
	)

	var buf = make([]byte, payloadLen+sizeLen+len(b))

	compressType := CompressType(0)
	if cp != nil {
		compressType = cp.TypeCode()
	}

	buf[0] = byte(compressType) | byte(c.TypeCode())
	// Write length of b into buf
	binary.BigEndian.PutUint32(buf[1:], uint32(length))
	// Copy encoded msg to buf
	copy(buf[5:], b)

	return buf, nil
}

func checkRecvPayload(codecType CodecType, compressType CompressType, recvCompress string, dc Decompressor) error {
	switch compressType {
	case COMPRESS_TYPE_NONE:
	case COMPRESS_TYPE_GZIP:
		if recvCompress == "" {
			return transport.StreamErrorf(codes.InvalidArgument, "grpc: received unexpected payload format %d", compressType)
		}
		if dc == nil || recvCompress != dc.Type() {
			return transport.StreamErrorf(codes.InvalidArgument, "grpc: Decompressor is not installed for grpc-encoding %q", recvCompress)
		}
	default:
		return transport.StreamErrorf(codes.InvalidArgument, "grpc: received unexpected payload format %d", compressType)
	}
	return nil
}

func recv(p *parser, s *transport.Stream, dg DecompressorGenerator, m interface{}) error {
	codecType, compressType, d, err := p.recvMsg()
	if err != nil {
		return err
	}

	//decompress
	var dc Decompressor
	if compressType == COMPRESS_TYPE_GZIP && dg != nil {
		dc = dg()
	}
	if err := checkRecvPayload(codecType, compressType, s.RecvCompress(), dc); err != nil {
		return err
	}
	if compressType == COMPRESS_TYPE_GZIP {
		d, err = dc.Do(bytes.NewReader(d))
		if err != nil {
			return transport.StreamErrorf(codes.Internal, "grpc: failed to decompress the received message %v", err)
		}
	}

	//unmarshal
	c, err := GetCodec(codecType)
	if err != nil {
		return err
	}
	if err := c.Unmarshal(d, m); err != nil {
		return transport.StreamErrorf(codes.Internal, "grpc: failed to unmarshal the received message %v", err)
	}
	return nil
}

// rpcError defines the status from an RPC.
type rpcError struct {
	code codes.Code
	desc string
}

func (e rpcError) Error() string {
	return fmt.Sprintf("rpc error: code = %d desc = %q", e.code, e.desc)
}

// Code returns the error code for err if it was produced by the rpc system.
// Otherwise, it returns codes.Unknown.
func Code(err error) codes.Code {
	if err == nil {
		return codes.OK
	}
	if e, ok := err.(rpcError); ok {
		return e.code
	}
	return codes.Unknown
}

// ErrorDesc returns the error description of err if it was produced by the rpc system.
// Otherwise, it returns err.Error() or empty string when err is nil.
func ErrorDesc(err error) string {
	if err == nil {
		return ""
	}
	if e, ok := err.(rpcError); ok {
		return e.desc
	}
	return err.Error()
}

// Errorf returns an error containing an error code and a description;
// Errorf returns nil if c is OK.
func Errorf(c codes.Code, format string, a ...interface{}) error {
	if c == codes.OK {
		return nil
	}
	return rpcError{
		code: c,
		desc: fmt.Sprintf(format, a...),
	}
}

// toRPCErr converts an error into a rpcError.
func toRPCErr(err error) error {
	switch e := err.(type) {
	case rpcError:
		return err
	case transport.StreamError:
		return rpcError{
			code: e.Code,
			desc: e.Desc,
		}
	case transport.ConnectionError:
		return rpcError{
			code: codes.Internal,
			desc: e.Desc,
		}
	}
	return Errorf(codes.Unknown, "%v", err)
}

// convertCode converts a standard Go error into its canonical code. Note that
// this is only used to translate the error returned by the server applications.
func convertCode(err error) codes.Code {
	switch err {
	case nil:
		return codes.OK
	case io.EOF:
		return codes.OutOfRange
	case io.ErrClosedPipe, io.ErrNoProgress, io.ErrShortBuffer, io.ErrShortWrite, io.ErrUnexpectedEOF:
		return codes.FailedPrecondition
	case os.ErrInvalid:
		return codes.InvalidArgument
	case context.Canceled:
		return codes.Canceled
	case context.DeadlineExceeded:
		return codes.DeadlineExceeded
	}
	switch {
	case os.IsExist(err):
		return codes.AlreadyExists
	case os.IsNotExist(err):
		return codes.NotFound
	case os.IsPermission(err):
		return codes.PermissionDenied
	}
	return codes.Unknown
}

const (
	// how long to wait after the first failure before retrying
	baseDelay = 1.0 * time.Second
	// upper bound of backoff delay
	maxDelay = 120 * time.Second
	// backoff increases by this factor on each retry
	backoffFactor = 1.6
	// backoff is randomized downwards by this factor
	backoffJitter = 0.2
)

func backoff(retries int) (t time.Duration) {
	if retries == 0 {
		return baseDelay
	}
	backoff, max := float64(baseDelay), float64(maxDelay)
	for backoff < max && retries > 0 {
		backoff *= backoffFactor
		retries--
	}
	if backoff > max {
		backoff = max
	}
	// Randomize backoff delays so that if a cluster of requests start at
	// the same time, they won't operate in lockstep.
	backoff *= 1 + backoffJitter*(rand.Float64()*2-1)
	if backoff < 0 {
		return 0
	}
	return time.Duration(backoff)
}
