/*
 *
 * Copyright 2024 gRPC authors.
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

package grpchttp2

import (
	"fmt"
	"testing"

	"github.com/google/go-cmp/cmp"
	"golang.org/x/net/http2/hpack"
	"google.golang.org/grpc/mem"
)

func (s) TestFramer_ReadFrame_Data(t *testing.T) {
	c := &testConn{}
	wantData := "test data"

	c.rbuf = append(c.rbuf, 0, 0, byte(len(wantData)), byte(FrameTypeData), 0)
	c.rbuf = appendUint32(c.rbuf, 1)
	c.rbuf = append(c.rbuf, []byte(wantData)...)

	fr := NewFramer(c, c, 0, nil)

	f, err := fr.ReadFrame()
	if err != nil {
		t.Fatalf("ReadFrame(): %v", err)
	}

	wantHdr := &FrameHeader{
		Size:     uint32(len(wantData)),
		Type:     FrameTypeData,
		Flags:    0,
		StreamID: 1,
	}
	if diff := cmp.Diff(f.Header(), wantHdr); diff != "" {
		t.Errorf("ReadFrame(): %v", diff)
	}
	f.Free()
}

func (s) TestFramer_ReadFrame_RSTStream(t *testing.T) {
	c := &testConn{}
	c.rbuf = append(c.rbuf, 0, 0, 4, byte(FrameTypeRSTStream), 0)
	c.rbuf = appendUint32(c.rbuf, 1)
	c.rbuf = appendUint32(c.rbuf, uint32(ErrCodeProtocol))

	fr := NewFramer(c, c, 0, nil)

	f, err := fr.ReadFrame()
	if err != nil {
		t.Fatalf("ReadFrame(): %v", err)
	}

	wantHdr := &FrameHeader{
		Size:     4,
		Type:     FrameTypeRSTStream,
		Flags:    0,
		StreamID: 1,
	}
	if diff := cmp.Diff(f.Header(), wantHdr); diff != "" {
		t.Errorf("ReadFrame(): %v", diff)
	}

	if rf := f.(*RSTStreamFrame); rf.ErrCode != ErrCodeProtocol {
		t.Errorf("ReadFrame(): Code: got %#x, want %#x", rf.ErrCode, ErrCodeProtocol)
	}

	f.Free()
}

func (s) TestFramer_ReadFrame_Settings(t *testing.T) {
	c := &testConn{}
	s := Setting{ID: SettingsHeaderTableSize, Value: 200}
	c.rbuf = append(c.rbuf, 0, 0, 6, byte(FrameTypeSettings), 0)
	c.rbuf = appendUint32(c.rbuf, 0)
	c.rbuf = append(c.rbuf, byte(s.ID>>8), byte(s.ID))
	c.rbuf = appendUint32(c.rbuf, s.Value)

	fr := NewFramer(c, c, 0, nil)

	f, err := fr.ReadFrame()
	if err != nil {
		t.Fatalf("ReadFrame(): %v", err)
	}
	defer f.Free()

	wantHdr := &FrameHeader{
		Size:     6,
		Type:     FrameTypeSettings,
		Flags:    0,
		StreamID: 0,
	}
	if diff := cmp.Diff(f.Header(), wantHdr); diff != "" {
		t.Errorf("ReadFrame(): %v", diff)
	}

	if sf := f.(*SettingsFrame); sf.Settings[0] != s {
		t.Errorf("ReadFrame(): Settings: got %v, want %v", sf.Settings[0], s)
	}
}

func (s) TestFramer_ReadFrame_Ping(t *testing.T) {
	c := &testConn{}
	d := []byte{1, 2, 3, 4, 5, 6, 7, 8}
	c.rbuf = append(c.rbuf, 0, 0, 8, byte(FrameTypePing), 0)
	c.rbuf = appendUint32(c.rbuf, 0)
	c.rbuf = append(c.rbuf, d...)

	fr := NewFramer(c, c, 0, nil)
	f, err := fr.ReadFrame()
	if err != nil {
		t.Fatalf("ReadFrame(): %v", err)
	}
	defer f.Free()

	wantHdr := &FrameHeader{
		Size:     8,
		Type:     FrameTypePing,
		Flags:    0,
		StreamID: 0,
	}
	if diff := cmp.Diff(f.Header(), wantHdr); diff != "" {
		t.Errorf("ReadFrame(): %v", diff)
	}

	for i, b := range d {
		if pf := f.(*PingFrame); b != pf.Data[i] {
			t.Errorf("ReadFrame(): Data[%d]: got %d, want %d", i, pf.Data[i], b)
		}
	}
}

func (s) TestFramer_ReadFrame_GoAway(t *testing.T) {
	c := &testConn{}
	d := "debug_data"
	// The length of data + 4 byte code + 4 byte streamID
	ln := len(d) + 8
	c.rbuf = append(c.rbuf, 0, 0, byte(ln), byte(FrameTypeGoAway), 0)
	c.rbuf = appendUint32(c.rbuf, 0)
	c.rbuf = appendUint32(c.rbuf, 2)
	c.rbuf = appendUint32(c.rbuf, uint32(ErrCodeFlowControl))
	c.rbuf = append(c.rbuf, []byte(d)...)

	fr := NewFramer(c, c, 0, nil)

	f, err := fr.ReadFrame()
	if err != nil {
		t.Fatalf("ReadFrame(): %v", err)
	}
	defer f.Free()

	wantHdr := &FrameHeader{
		Size:     uint32(ln),
		Type:     FrameTypeGoAway,
		Flags:    0,
		StreamID: 0,
	}
	if diff := cmp.Diff(f.Header(), wantHdr); diff != "" {
		t.Errorf("ReadFrame(): %v", diff)
	}
	gf := f.(*GoAwayFrame)
	if gf.LastStreamID != 2 {
		t.Errorf("ReadFrame(): LastStreamID: got %d, want %d", gf.LastStreamID, 2)
	}
	if gf.ErrCode != ErrCodeFlowControl {
		t.Errorf("ReadFrame(): Code: got %#x, want %#x", gf.ErrCode, ErrCodeFlowControl)
	}
	if string(gf.DebugData) != d {
		t.Errorf("ReadFrame(): DebugData: got %q, want %q", string(gf.DebugData), d)
	}
}

func (s) TestFramer_ReadFrame_WindowUpdate(t *testing.T) {
	c := &testConn{}
	c.rbuf = append(c.rbuf, 0, 0, 4, byte(FrameTypeWindowUpdate), 0)
	c.rbuf = appendUint32(c.rbuf, 1)
	c.rbuf = appendUint32(c.rbuf, 100)

	fr := NewFramer(c, c, 0, nil)

	f, err := fr.ReadFrame()
	if err != nil {
		t.Fatalf("ReadFrame(): %v", err)
	}
	defer f.Free()

	wantHdr := &FrameHeader{
		Size:     4,
		Type:     FrameTypeWindowUpdate,
		Flags:    0,
		StreamID: 1,
	}
	if diff := cmp.Diff(f.Header(), wantHdr); diff != "" {
		t.Errorf("ReadFrame(): %v", diff)
	}

	wf := f.(*WindowUpdateFrame)
	if wf.Inc != 100 {
		t.Errorf("ReadFrame(): Inc: got %d, want %d", wf.Inc, 100)
	}
}

func (s) TestFramer_ReadFrame_MetaHeaders(t *testing.T) {
	fields := []hpack.HeaderField{
		{Name: "foo", Value: "bar"},
		{Name: "baz", Value: "qux"},
	}

	c := &testConn{}
	enc := hpack.NewEncoder(c)
	for _, field := range fields {
		enc.WriteField(field)
	}
	half1 := c.wbuf[0 : len(c.wbuf)/2]
	half2 := c.wbuf[len(c.wbuf)/2:]

	c.rbuf = append(c.rbuf, 0, 0, byte(len(half1)), byte(FrameTypeHeaders), 0)
	c.rbuf = appendUint32(c.rbuf, 1)
	// Copy half of the data written by the encoder into the reading buf
	c.rbuf = append(c.rbuf, half1...)

	// Add Continuation Frame to test merging
	c.rbuf = append(c.rbuf, 0, 0, byte(len(half2)), byte(FrameTypeContinuation), byte(FlagContinuationEndHeaders))
	c.rbuf = appendUint32(c.rbuf, 1)
	// Copy data written by the encoder into the reading buf
	c.rbuf = append(c.rbuf, half2...)

	f := NewFramer(c, c, 0, nil)
	fr, err := f.ReadFrame()
	if err != nil {
		t.Fatalf("ReadFrame(): %v", err)
	}

	mf, ok := fr.(*MetaHeadersFrame)
	if !ok {
		t.Errorf("ReadFrame(): Type: expected MetaHeadersFrame, got %T", fr)
	}
	if len(mf.Fields) != 2 {
		t.Fatalf("ReadFrame(): Fields: got %d, want %d", len(mf.Fields), 1)
	}
	for i, field := range fields {
		if field.Name != mf.Fields[i].Name {
			t.Errorf("ReadFrame(): Fields[%d].Name: got %q, want %q", i, mf.Fields[i].Name, field.Name)
		}
		if field.Value != mf.Fields[i].Value {
			t.Errorf("ReadFrame(): Fields[%d].Value: got %q, want %q", i, mf.Fields[i].Value, field.Value)
		}
	}
}

// Tests and verifies that a Data Frame is correctly written.
func (s) TestFramer_WriteData_MultipleSlices(t *testing.T) {
	testData := []byte("test data")
	testData1 := []byte("test")
	testData2 := []byte(" data")
	tests := []struct {
		name string
		data mem.BufferSlice
		want string
	}{
		{
			name: "empty",
			data: mem.BufferSlice{},
			want: "",
		},
		{
			name: "single",
			data: mem.BufferSlice{mem.NewBuffer(&testData, nil)},
			want: "test data",
		},
		{
			name: "multiple",
			data: mem.BufferSlice{
				mem.NewBuffer(&testData1, nil),
				mem.NewBuffer(&testData2, nil),
			},
			want: "test data",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			c := &testConn{}
			f := NewFramer(c, c, 0, nil)
			f.WriteData(1, false, nil, test.data)

			wantHdr := &FrameHeader{
				Size:     uint32(test.data.Len()),
				Type:     FrameTypeData,
				Flags:    0,
				StreamID: 1,
			}
			gotHdr := parseWrittenHeader(c.wbuf[:9])
			if diff := cmp.Diff(gotHdr, wantHdr); diff != "" {
				t.Errorf("WriteData() (-got, +want): %s", diff)
			}

			if string(c.wbuf[9:]) != test.want {
				t.Errorf("WriteData(): Data: got %q, want %q", string(c.wbuf[9:]), test.want)
			}
		})
	}
}

// Tests and verifies that a Headers Frame and all its flag permutations are
// correctly written.
func (s) TestFramer_WriteHeaders(t *testing.T) {
	tests := []struct {
		name       string
		endStream  bool
		endHeaders bool
	}{
		{name: "no flags", endStream: false, endHeaders: false},
		{name: "endheaders", endStream: false, endHeaders: true},
		{name: "endstream and endheaders", endStream: true, endHeaders: true},
		{name: "endstream", endStream: true, endHeaders: false},
	}
	wantData := "test data"

	for _, test := range tests {
		t.Run(fmt.Sprintf(test.name, test.endStream, test.endHeaders), func(t *testing.T) {
			c := &testConn{}
			f := NewFramer(c, c, 0, nil)

			f.WriteHeaders(1, test.endStream, test.endHeaders, []byte(wantData))

			var wantFlags Flag
			if test.endStream {
				wantFlags |= FlagHeadersEndStream
			}
			if test.endHeaders {
				wantFlags |= FlagHeadersEndHeaders
			}
			wantHdr := &FrameHeader{
				Size:     uint32(len(wantData)),
				Type:     FrameTypeHeaders,
				Flags:    wantFlags,
				StreamID: 1,
			}
			gotHdr := parseWrittenHeader(c.wbuf[:9])
			if diff := cmp.Diff(gotHdr, wantHdr); diff != "" {
				t.Errorf("WriteHeaders() (-got, +want): %s", diff)
			}

			if data := string(c.wbuf[9:]); data != wantData {
				t.Errorf("WriteHeaders(): Data: got %q, want %q", data, wantData)
			}
		})

	}
}

// Tests and verifies that a RSTStream Frame is correctly written.
func (s) TestFramer_WriteRSTStream(t *testing.T) {
	c := &testConn{}
	f := NewFramer(c, c, 0, nil)
	f.WriteRSTStream(1, ErrCodeProtocol)

	wantHdr := &FrameHeader{
		Size:     4,
		Type:     FrameTypeRSTStream,
		Flags:    0,
		StreamID: 1,
	}
	gotHdr := parseWrittenHeader(c.wbuf[:9])
	if diff := cmp.Diff(gotHdr, wantHdr); diff != "" {
		t.Errorf("WriteRSTStream() (-got, +want): %s", diff)
	}

	if errCode := readUint32(c.wbuf[9:13]); errCode != uint32(ErrCodeProtocol) {
		t.Errorf("WriteRSTStream(): SettingID: got %d, want %d", errCode, ErrCodeProtocol)
	}
}

// Tests and verifies that a Settings Frame is correctly written.
func (s) TestFramer_WriteSettings(t *testing.T) {
	c := &testConn{}
	f := NewFramer(c, c, 0, nil)
	f.WriteSettings(Setting{ID: SettingsHeaderTableSize, Value: 200})

	wantHdr := &FrameHeader{
		Size:     6,
		Type:     FrameTypeSettings,
		Flags:    0,
		StreamID: 0,
	}
	gotHdr := parseWrittenHeader(c.wbuf[:9])
	if diff := cmp.Diff(gotHdr, wantHdr); diff != "" {
		t.Errorf("WriteSettings() (-got, +want): %s", diff)
	}

	if settingID := readUint16(c.wbuf[9:11]); settingID != uint16(SettingsHeaderTableSize) {
		t.Errorf("WriteSettings(): SettingID: got %d, want %d", settingID, SettingsHeaderTableSize)
	}
	if settingVal := readUint32(c.wbuf[11:15]); settingVal != 200 {
		t.Errorf("WriteSettings(): SettingVal: got %d, want %d", settingVal, 200)
	}
}

// Tests and verifies that a Settings Frame with the ack flag is correctly
// written.
func (s) TestFramer_WriteSettingsAck(t *testing.T) {
	c := &testConn{}
	f := NewFramer(c, c, 0, nil)
	f.WriteSettingsAck()

	wantHdr := &FrameHeader{
		Size:     0,
		Type:     FrameTypeSettings,
		Flags:    FlagSettingsAck,
		StreamID: 0,
	}
	gotHdr := parseWrittenHeader(c.wbuf[:9])
	if diff := cmp.Diff(gotHdr, wantHdr); diff != "" {
		t.Errorf("WriteSettingsAck() (-got, +want): %s", diff)
	}

}

// Tests and verifies that a Ping Frame is correctly written with its flag
// permutations.
func (s) TestFramer_WritePing(t *testing.T) {
	wantData := [8]byte{1, 2, 3, 4, 5, 6, 7, 8}
	acks := []bool{true, false}

	for _, ack := range acks {
		t.Run(fmt.Sprintf("ack=%v", ack), func(t *testing.T) {
			c := &testConn{}
			f := NewFramer(c, c, 0, nil)

			f.WritePing(ack, wantData)
			var wantFlags Flag
			if ack {
				wantFlags |= FlagPingAck
			}
			wantHdr := &FrameHeader{
				Size:     8,
				Type:     FrameTypePing,
				Flags:    wantFlags,
				StreamID: 0,
			}
			gotHdr := parseWrittenHeader(c.wbuf[:9])
			if diff := cmp.Diff(gotHdr, wantHdr); diff != "" {
				t.Errorf("WritePing() (-got, +want): %s", diff)
			}

			data := c.wbuf[9:]
			for i := range data {
				if data[i] != wantData[i] {
					t.Errorf("WritePing(): Data[%d]: got %d, want %d", i, data[i], wantData[i])
				}
			}
			c.wbuf = c.wbuf[:0]
		})
	}
}

// Tests and verifies that a GoAway Frame is correctly written.
func (s) TestFramer_WriteGoAway(t *testing.T) {
	c := &testConn{}
	f := NewFramer(c, c, 0, nil)
	f.WriteGoAway(2, ErrCodeFlowControl, []byte("debug_data"))

	wantHdr := &FrameHeader{
		Size:     18,
		Type:     FrameTypeGoAway,
		Flags:    0,
		StreamID: 0,
	}
	gotHdr := parseWrittenHeader(c.wbuf[:9])
	if diff := cmp.Diff(gotHdr, wantHdr); diff != "" {
		t.Errorf("WriteGoAway() (-got, +want): %s", diff)
	}

	if lastStream := readUint32(c.wbuf[9:13]); lastStream != 2 {
		t.Errorf("WriteGoAway(): LastStreamID: got %d, want %d", lastStream, 2)
	}
	if code := ErrCode(readUint32(c.wbuf[13:17])); code != ErrCodeFlowControl {
		t.Errorf("WriteGoAway(): Code: got %d, want %d", code, ErrCodeFlowControl)
	}
	if data := string(c.wbuf[17:]); data != "debug_data" {
		t.Errorf("WriteGoAway(): Data: got %q, want %q", data, "debug_data")
	}
}

// Tests and verifies that a WindowUpdate Frame is correctly written.
func (s) TestFramer_WriteWindowUpdate(t *testing.T) {
	c := &testConn{}
	f := NewFramer(c, c, 0, nil)
	f.WriteWindowUpdate(1, 2)

	wantHdr := &FrameHeader{
		Size:     4,
		Type:     FrameTypeWindowUpdate,
		Flags:    0,
		StreamID: 1,
	}
	gotHdr := parseWrittenHeader(c.wbuf[:9])
	if diff := cmp.Diff(gotHdr, wantHdr); diff != "" {
		t.Errorf("WriteWindowUpdate() (-got, +want): %s", diff)
	}

	if inc := readUint32(c.wbuf[9:13]); inc != 2 {
		t.Errorf("WriteWindowUpdate(): Inc: got %d, want %d", inc, 2)
	}
}

// Tests and verifies that a Continuation Frame is correctly written with its
// flag permutations.
func (s) TestFramer_WriteContinuation(t *testing.T) {
	wantData := "hdr block"
	endHeaders := []struct {
		name       string
		endHeaders bool
	}{
		{name: "no flags", endHeaders: false},
		{name: "endheaders", endHeaders: true},
	}

	for _, test := range endHeaders {
		t.Run(test.name, func(t *testing.T) {
			c := &testConn{}
			f := NewFramer(c, c, 0, nil)
			f.WriteContinuation(1, test.endHeaders, []byte("hdr block"))
			var wantFlags Flag
			if test.endHeaders {
				wantFlags |= FlagContinuationEndHeaders
			}
			wantHdr := &FrameHeader{
				Size:     uint32(len(wantData)),
				Type:     FrameTypeContinuation,
				Flags:    wantFlags,
				StreamID: 1,
			}
			gotHdr := parseWrittenHeader(c.wbuf[:9])
			if diff := cmp.Diff(gotHdr, wantHdr); diff != "" {
				t.Errorf("WriteContinuation() (-got, +want): %s", diff)
			}

			if data := string(c.wbuf[9:]); data != wantData {
				t.Errorf("WriteContinuation(): Data: got %q, want %q", data, wantData)
			}
			c.wbuf = c.wbuf[:0]
		})

	}
}
