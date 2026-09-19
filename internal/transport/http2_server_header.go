/*
 *
 * Copyright 2026 gRPC authors.
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

package transport

import (
	"errors"
	"fmt"
	"strings"

	"golang.org/x/net/http/httpguts"
	"golang.org/x/net/http2"
	"golang.org/x/net/http2/hpack"
)

const serverDefaultMaxHeaderListSize = 16 << 20

type serverHeaderDecoder struct {
	decoder           *hpack.Decoder
	maxHeaderListSize uint32
	frame             http2.MetaHeadersFrame
	remainSize        uint32
	sawRegular        bool
	invalid           error
	emit              func(hpack.HeaderField)
}

func newServerHeaderDecoder(decoder *hpack.Decoder, maxHeaderListSize uint32) *serverHeaderDecoder {
	if maxHeaderListSize == 0 {
		maxHeaderListSize = serverDefaultMaxHeaderListSize
	}
	d := &serverHeaderDecoder{
		decoder:           decoder,
		maxHeaderListSize: maxHeaderListSize,
	}
	d.emit = d.emitField
	return d
}

func (d *serverHeaderDecoder) reset(frame *http2.HeadersFrame) {
	clear(d.frame.Fields)
	d.frame.HeadersFrame = frame
	d.frame.Fields = d.frame.Fields[:0]
	d.frame.Truncated = false
	d.remainSize = d.maxHeaderListSize
	d.sawRegular = false
	d.invalid = nil

	d.decoder.SetEmitEnabled(true)
	maxStringLength := int(d.maxHeaderListSize)
	if maxStringLength < 0 {
		maxStringLength = 0
	}
	d.decoder.SetMaxStringLength(maxStringLength)
	d.decoder.SetEmitFunc(d.emit)
}

func (d *serverHeaderDecoder) emitField(field hpack.HeaderField) {
	if !httpguts.ValidHeaderFieldValue(field.Value) {
		d.invalid = fmt.Errorf(
			"invalid header field value for %q",
			field.Name,
		)
	}

	if strings.HasPrefix(field.Name, ":") {
		if d.sawRegular {
			d.invalid = errors.New(
				"pseudo header field after regular",
			)
		}
	} else {
		d.sawRegular = true
		if !validServerWireHeaderFieldName(field.Name) {
			d.invalid = fmt.Errorf(
				"invalid header field name %q",
				field.Name,
			)
		}
	}

	if d.invalid != nil {
		d.decoder.SetEmitEnabled(false)
		return
	}

	size := field.Size()
	if size > d.remainSize {
		d.decoder.SetEmitEnabled(false)
		d.frame.Truncated = true
		d.remainSize = 0
		return
	}

	d.remainSize -= size
	d.frame.Fields = append(d.frame.Fields, field)
}

func (d *serverHeaderDecoder) read(
	f *framer,
	header http2.FrameHeader,
) (*http2.MetaHeadersFrame, error) {
	metaDecoder := f.fr.ReadMetaHeaders
	f.fr.ReadMetaHeaders = nil

	frame, err := f.fr.ReadFrameForHeader(header)

	f.fr.ReadMetaHeaders = metaDecoder

	if err != nil {
		f.errDetail = f.fr.ErrorDetail()
		return nil, err
	}

	headers, ok := frame.(*http2.HeadersFrame)
	if !ok {
		return nil, fmt.Errorf(
			"decoded %T, want *http2.HeadersFrame",
			frame,
		)
	}

	d.reset(headers)

	var current interface {
		HeaderBlockFragment() []byte
		HeadersEnded() bool
	} = headers

	for {
		fragment := current.HeaderBlockFragment()

		if int64(len(fragment)) > int64(2*d.remainSize) {
			return &d.frame,
				http2.ConnectionError(http2.ErrCodeProtocol)
		}

		if d.invalid != nil {
			return &d.frame,
				http2.ConnectionError(http2.ErrCodeProtocol)
		}

		if _, err := d.decoder.Write(fragment); err != nil {
			return &d.frame,
				http2.ConnectionError(http2.ErrCodeCompression)
		}

		if current.HeadersEnded() {
			break
		}

		next, err := f.fr.ReadFrame()
		if err != nil {
			f.errDetail = f.fr.ErrorDetail()
			return nil, err
		}

		current = next.(*http2.ContinuationFrame)
	}

	if err := d.decoder.Close(); err != nil {
		return &d.frame,
			http2.ConnectionError(http2.ErrCodeCompression)
	}

	if d.invalid != nil {
		f.errDetail = d.invalid
		return nil, http2.StreamError{
			StreamID: d.frame.StreamID,
			Code:     http2.ErrCodeProtocol,
			Cause:    d.invalid,
		}
	}

	if err := checkServerPseudos(d.frame.Fields); err != nil {
		f.errDetail = err
		return nil, http2.StreamError{
			StreamID: d.frame.StreamID,
			Code:     http2.ErrCodeProtocol,
			Cause:    err,
		}
	}

	return &d.frame, nil
}

func (f *framer) readServerFrame() (any, error) {
	f.errDetail = nil

	header, err := f.fr.ReadFrameHeader()
	if err != nil {
		f.errDetail = f.fr.ErrorDetail()
		return nil, err
	}

	if header.Type == http2.FrameData {
		err = f.readDataFrame(header)
		return &f.dataFrame, err
	}

	if header.Type == http2.FrameHeaders {
		if f.serverHeader == nil {
			f.serverHeader = newServerHeaderDecoder(
				f.fr.ReadMetaHeaders,
				f.fr.MaxHeaderListSize,
			)
		}
		frame, err := f.serverHeader.read(f, header)
		if err != nil {
			return nil, err
		}
		return frame, nil
	}

	frame, err := f.fr.ReadFrameForHeader(header)
	if err != nil {
		f.errDetail = f.fr.ErrorDetail()
		return nil, err
	}

	return frame, nil
}

func validServerWireHeaderFieldName(value string) bool {
	if len(value) == 0 {
		return false
	}

	for _, r := range value {
		if !httpguts.IsTokenRune(r) ||
			('A' <= r && r <= 'Z') {
			return false
		}
	}

	return true
}

func checkServerPseudos(fields []hpack.HeaderField) error {
	var request bool
	var response bool

	for i, field := range fields {
		if !field.IsPseudo() {
			break
		}

		switch field.Name {
		case ":method",
			":path",
			":scheme",
			":authority",
			":protocol":
			request = true

		case ":status":
			response = true

		default:
			return fmt.Errorf(
				"invalid pseudo-header %q",
				field.Name,
			)
		}

		for _, previous := range fields[:i] {
			if field.Name == previous.Name {
				return fmt.Errorf(
					"duplicate pseudo-header %q",
					field.Name,
				)
			}
		}
	}

	if request && response {
		return errors.New(
			"mix of request and response pseudo headers",
		)
	}

	return nil
}
