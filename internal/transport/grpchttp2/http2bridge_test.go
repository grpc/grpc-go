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
	"errors"
	"fmt"
	"testing"

	"golang.org/x/net/http2/hpack"
)

// testConn is a test utility which provides an io.Writer and io.Reader
// interface and access to its internal buffers for testing.
type testConn struct {
	wbuf []byte
	rbuf []byte
}

func (c *testConn) Write(p []byte) (int, error) {
	c.wbuf = append(c.wbuf, p...)
	return len(p), nil
}

func (c *testConn) Read(p []byte) (int, error) {
	n := copy(p, c.rbuf)
	c.rbuf = c.rbuf[n:]
	return n, nil
}

func appendUint32(b []byte, x uint32) []byte {
	return append(b, byte(x>>24), byte(x>>16), byte(x>>8), byte(x))
}

func readUint32(b []byte) uint32 {
	return uint32(b[0])<<24 | uint32(b[1])<<16 | uint32(b[2])<<8 | uint32(b[3])
}

func readUint24(b []byte) int {
	return int(b[0])<<16 | int(b[1])<<8 | int(b[2])
}

func readUint16(b []byte) uint16 {
	return uint16(b[0])<<8 | uint16(b[1])
}

type wantHeader struct {
	wantSize      int
	wantFrameType FrameType
	wantFlag      Flag
	wantStreamID  uint32
}

// checkWrittenHeader takes a byte buffer representing a written frame header and
// compares its values to the passed values.
func checkWrittenHeader(gotHdr []byte, wantHdr wantHeader) []error {
	var errors []error
	if gotSize := readUint24(gotHdr[0:3]); gotSize != wantHdr.wantSize {
		errors = append(errors, fmt.Errorf("Size: got %d, want %d", gotSize, wantHdr.wantSize))
	}
	if gotType := FrameType(gotHdr[3]); gotType != wantHdr.wantFrameType {
		errors = append(errors, fmt.Errorf("Type: got %#x, want %#x", gotType, wantHdr.wantFrameType))
	}
	if gotFlag := Flag(gotHdr[4]); gotFlag != wantHdr.wantFlag {
		errors = append(errors, fmt.Errorf("Flags: got %#x, want %#x", gotFlag, wantHdr.wantFlag))
	}
	if gotSID := readUint32(gotHdr[5:9]); gotSID != wantHdr.wantStreamID {
		errors = append(errors, fmt.Errorf("StreamID: got %d, want %d", gotSID, wantHdr.wantStreamID))
	}
	return errors
}

// checkReadHeader takes a FrameHeader struct and compares its values to the
// passed values.
func checkReadHeader(gotHdr *FrameHeader, wantHdr wantHeader) []error {
	var errors []error
	if gotHdr.Size != uint32(wantHdr.wantSize) {
		errors = append(errors, fmt.Errorf("Size: got %d, want %d", gotHdr.Size, wantHdr.wantSize))
	}
	if gotHdr.Type != wantHdr.wantFrameType {
		errors = append(errors, fmt.Errorf("Type: got %#x, want %#x", gotHdr.Type, wantHdr.wantFrameType))
	}
	if gotHdr.Flags != wantHdr.wantFlag {
		errors = append(errors, fmt.Errorf("Flags: got %#x, want %#x", gotHdr.Flags, wantHdr.wantFlag))
	}
	if gotHdr.StreamID != wantHdr.wantStreamID {
		errors = append(errors, fmt.Errorf("StreamID: got %d, want %d", gotHdr.StreamID, wantHdr.wantStreamID))
	}
	return errors
}

// Tests and verifies that the framer correctly reads a Data Frame.
func (s) TestBridge_ReadFrame_Data(t *testing.T) {
	c := &testConn{}
	recvData := "test data"
	c.rbuf = append(c.rbuf, 0, 0, byte(len(recvData)), byte(FrameTypeData), byte(FlagDataEndStream))
	c.rbuf = appendUint32(c.rbuf, 1)
	c.rbuf = append(c.rbuf, []byte(recvData)...)

	f := NewFramerBridge(c, c, 0, nil)
	fr, err := f.ReadFrame()
	if err != nil {
		t.Fatalf("ReadFrame(): %v", err)
	}

	h := fr.Header()
	wantHdr := wantHeader{
		wantSize:      len(recvData),
		wantFrameType: FrameTypeData,
		wantFlag:      FlagDataEndStream,
		wantStreamID:  1,
	}
	if errs := checkReadHeader(h, wantHdr); len(errs) > 0 {
		t.Errorf("ReadFrame():\n%s", errors.Join(errs...))
	}
	df := fr.(*DataFrame)
	if string(df.Data) != recvData {
		t.Errorf("ReadFrame(): Data: got %q, want %q", string(df.Data), recvData)
	}
	df.Free()
}

// Tests and verifies that the framer correctly reads a RSTStream Frame.
func (s) TestBridge_ReadFrame_RSTStream(t *testing.T) {
	c := &testConn{}
	c.rbuf = append(c.rbuf, 0, 0, 4, byte(FrameTypeRSTStream), 0)
	c.rbuf = appendUint32(c.rbuf, 1)
	c.rbuf = appendUint32(c.rbuf, uint32(ErrCodeProtocol))

	f := NewFramerBridge(c, c, 0, nil)
	fr, err := f.ReadFrame()
	if err != nil {
		t.Fatalf("ReadFrame(): %v", err)
	}

	h := fr.Header()
	wantHdr := wantHeader{
		wantSize:      4,
		wantFrameType: FrameTypeRSTStream,
		wantFlag:      0,
		wantStreamID:  1,
	}
	if errs := checkReadHeader(h, wantHdr); len(errs) > 0 {
		t.Errorf("ReadFrame():\n%s", errors.Join(errs...))
	}
	rf := fr.(*RSTStreamFrame)
	if rf.Code != ErrCodeProtocol {
		t.Errorf("ReadFrame(): Code: got %#x, want %#x", rf.Code, ErrCodeProtocol)
	}
}

// Tests and verifies that the framer correctly reads a Settings Frame.
func (s) TestBridge_ReadFrame_Settings(t *testing.T) {
	c := &testConn{}
	s := Setting{ID: SettingsHeaderTableSize, Value: 200}
	c.rbuf = append(c.rbuf, 0, 0, 6, byte(FrameTypeSettings), 0)
	c.rbuf = appendUint32(c.rbuf, 0)
	c.rbuf = append(c.rbuf, byte(s.ID>>8), byte(s.ID))
	c.rbuf = appendUint32(c.rbuf, s.Value)

	f := NewFramerBridge(c, c, 0, nil)
	fr, err := f.ReadFrame()
	if err != nil {
		t.Fatalf("ReadFrame(): %v", err)
	}

	h := fr.Header()
	wantHdr := wantHeader{
		wantSize:      6,
		wantFrameType: FrameTypeSettings,
		wantFlag:      0,
		wantStreamID:  0,
	}
	if errs := checkReadHeader(h, wantHdr); len(errs) > 0 {
		t.Errorf("ReadFrame():\n%s", errors.Join(errs...))
	}

	sf := fr.(*SettingsFrame)
	if len(sf.Settings) != 1 {
		t.Fatalf("ReadFrame(): Settings: got %d, want %d", len(sf.Settings), 1)
	}
	if sf.Settings[0] != s {
		t.Errorf("ReadFrame(): Settings: got %v, want %v", sf.Settings[0], s)
	}
}

// Tests and verifies that the framer correctly reads a Ping Frame.
func (s) TestBridge_ReadFrame_Ping(t *testing.T) {
	c := &testConn{}
	d := []byte{1, 2, 3, 4, 5, 6, 7, 8}
	c.rbuf = append(c.rbuf, 0, 0, 8, byte(FrameTypePing), 0)
	c.rbuf = appendUint32(c.rbuf, 0)
	c.rbuf = append(c.rbuf, d...)

	f := NewFramerBridge(c, c, 0, nil)
	fr, err := f.ReadFrame()
	if err != nil {
		t.Fatalf("ReadFrame(): %v", err)
	}

	h := fr.Header()
	wantHdr := wantHeader{
		wantSize:      8,
		wantFrameType: FrameTypePing,
		wantFlag:      0,
		wantStreamID:  0,
	}
	if errs := checkReadHeader(h, wantHdr); len(errs) > 0 {
		t.Errorf("ReadFrame():\n%s", errors.Join(errs...))
	}

	pf := fr.(*PingFrame)
	for i := range pf.Data {
		if pf.Data[i] != d[i] {
			t.Errorf("ReadFrame(): Data[%d]: got %d, want %d", i, pf.Data[i], d[i])
		}
	}
	pf.Free()
}

// Tests and verifies that the framer correctly reads a GoAway Frame.
func (s) TestBridge_ReadFrame_GoAway(t *testing.T) {
	c := &testConn{}
	d := "debug_data"
	// The length of data + 4 byte code + 4 byte streamID
	ln := len(d) + 8
	c.rbuf = append(c.rbuf, 0, 0, byte(ln), byte(FrameTypeGoAway), 0)
	c.rbuf = appendUint32(c.rbuf, 0)
	c.rbuf = appendUint32(c.rbuf, 2)
	c.rbuf = appendUint32(c.rbuf, uint32(ErrCodeFlowControl))
	c.rbuf = append(c.rbuf, []byte(d)...)
	f := NewFramerBridge(c, c, 0, nil)

	fr, err := f.ReadFrame()
	if err != nil {
		t.Fatalf("ReadFrame(): %v", err)
	}

	h := fr.Header()
	wantHdr := wantHeader{
		wantSize:      ln,
		wantFrameType: FrameTypeGoAway,
		wantFlag:      0,
		wantStreamID:  0,
	}
	if errs := checkReadHeader(h, wantHdr); len(errs) > 0 {
		t.Errorf("ReadFrame():\n%s", errors.Join(errs...))
	}

	gf := fr.(*GoAwayFrame)
	if gf.LastStreamID != 2 {
		t.Errorf("ReadFrame(): LastStreamID: got %d, want %d", gf.LastStreamID, 2)
	}
	if gf.Code != ErrCodeFlowControl {
		t.Errorf("ReadFrame(): Code: got %#x, want %#x", gf.Code, ErrCodeFlowControl)
	}
	if string(gf.DebugData) != d {
		t.Errorf("ReadFrame(): DebugData: got %q, want %q", string(gf.DebugData), d)
	}
	gf.Free()
}

// Tests and verifies that the framer correctly reads a WindowUpdate Frame.
func (s) TestBridge_ReadFrame_WindowUpdate(t *testing.T) {
	c := &testConn{}
	c.rbuf = append(c.rbuf, 0, 0, 4, byte(FrameTypeWindowUpdate), 0)
	c.rbuf = appendUint32(c.rbuf, 1)
	c.rbuf = appendUint32(c.rbuf, 100)

	f := NewFramerBridge(c, c, 0, nil)
	fr, err := f.ReadFrame()
	if err != nil {
		t.Fatalf("ReadFrame(): %v", err)
	}

	h := fr.Header()
	wantHdr := wantHeader{
		wantSize:      4,
		wantFrameType: FrameTypeWindowUpdate,
		wantFlag:      0,
		wantStreamID:  1,
	}
	checkReadHeader(h, wantHdr)

	wf := fr.(*WindowUpdateFrame)
	if wf.Inc != 100 {
		t.Errorf("ReadFrame(): Inc: got %d, want %d", wf.Inc, 1)
	}
}

// Tests and verifies that the framer correctly merges Headers and Continuation
// Frames into a single MetaHeaders Frame.
func (s) TestBridge_ReadFrame_MetaHeaders(t *testing.T) {
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

	f := NewFramerBridge(c, c, 0, nil)
	fr, err := f.ReadFrame()
	if err != nil {
		t.Fatalf("ReadFrame(): %v", err)
	}

	mf, ok := fr.(*MetaHeadersFrame)
	if !ok {
		t.Errorf("ReadFrame(): Type: expected MetaHeadersFrame, got %T", fr)
	}
	if len(mf.Fields) != 2 {
		t.Errorf("ReadFrame(): Fields: got %d, want %d", len(mf.Fields), 1)
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
func (s) TestBridge_WriteData(t *testing.T) {
	c := &testConn{}
	wantData := "test data"
	testBuf := [][]byte{[]byte("test"), []byte(" data")}
	f := NewFramerBridge(c, c, 0, nil)
	f.WriteData(1, false, testBuf...)
	wantHdr := wantHeader{
		wantSize:      len(wantData),
		wantFrameType: FrameTypeData,
		wantFlag:      0,
		wantStreamID:  1,
	}
	if errs := checkWrittenHeader(c.wbuf[0:9], wantHdr); len(errs) > 0 {
		t.Errorf("WriteData():\n%s", errors.Join(errs...))
	}
	if string(c.wbuf[9:]) != wantData {
		t.Errorf("WriteData(): Data: got %q, want %q", string(c.wbuf[9:]), wantData)
	}
}

// Tests and verifies that a Headers Frame and all its flag permutations are
// correctly written.
func (s) TestBridge_WriteHeaders(t *testing.T) {
	tests := []struct {
		endStream  bool
		endHeaders bool
	}{
		{endStream: false, endHeaders: false},
		{endStream: true, endHeaders: false},
		{endStream: false, endHeaders: true},
		{endStream: true, endHeaders: true},
	}
	wantData := "test data"

	for _, test := range tests {
		t.Run(fmt.Sprintf("endStream=%v, endHeaders=%v", test.endStream, test.endHeaders), func(t *testing.T) {
			c := &testConn{}
			f := NewFramerBridge(c, c, 0, nil)

			f.WriteHeaders(1, test.endStream, test.endHeaders, []byte(wantData))

			var wantFlags Flag
			if test.endStream {
				wantFlags |= FlagHeadersEndStream
			}
			if test.endHeaders {
				wantFlags |= FlagHeadersEndHeaders
			}
			wantHdr := wantHeader{
				wantSize:      len(wantData),
				wantFrameType: FrameTypeHeaders,
				wantFlag:      wantFlags,
				wantStreamID:  1,
			}
			if errs := checkWrittenHeader(c.wbuf[0:9], wantHdr); len(errs) > 0 {
				t.Errorf("WriteHeaders():\n%s", errors.Join(errs...))
			}
			if data := string(c.wbuf[9:]); data != wantData {
				t.Errorf("WriteHeaders(): Data: got %q, want %q", data, wantData)
			}
			c.wbuf = c.wbuf[:0]
		})

	}
}

// Tests and verifies that a RSTStream Frame is correctly written.
func (s) TestBridge_WriteRSTStream(t *testing.T) {
	c := &testConn{}
	f := NewFramerBridge(c, c, 0, nil)
	f.WriteRSTStream(1, ErrCodeProtocol)

	wantHdr := wantHeader{
		wantSize:      4,
		wantFrameType: FrameTypeRSTStream,
		wantFlag:      0,
		wantStreamID:  1,
	}
	if errs := checkWrittenHeader(c.wbuf[0:9], wantHdr); len(errs) > 0 {
		t.Errorf("WriteRSTStream():\n%s", errors.Join(errs...))
	}
	if errCode := readUint32(c.wbuf[9:13]); errCode != uint32(ErrCodeProtocol) {
		t.Errorf("WriteRSTStream(): SettingID: got %d, want %d", errCode, ErrCodeProtocol)
	}
}

// Tests and verifies that a Settings Frame is correctly written.
func (s) TestBridge_WriteSettings(t *testing.T) {
	c := &testConn{}
	f := NewFramerBridge(c, c, 0, nil)
	f.WriteSettings(Setting{ID: SettingsHeaderTableSize, Value: 200})

	wantHeader := wantHeader{
		wantSize:      6,
		wantFrameType: FrameTypeSettings,
		wantFlag:      0,
		wantStreamID:  0,
	}
	if errs := checkWrittenHeader(c.wbuf[0:9], wantHeader); len(errs) > 0 {
		t.Errorf("WriteSettings():\n%s", errors.Join(errs...))
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
func (s) TestBridge_WriteSettingsAck(t *testing.T) {
	c := &testConn{}
	f := NewFramerBridge(c, c, 0, nil)
	f.WriteSettingsAck()

	wantHdr := wantHeader{
		wantSize:      0,
		wantFrameType: FrameTypeSettings,
		wantFlag:      FlagSettingsAck,
		wantStreamID:  0,
	}
	if errs := checkWrittenHeader(c.wbuf[0:9], wantHdr); len(errs) > 0 {
		t.Errorf("WriteSettingsAck():\n%s", errors.Join(errs...))
	}
}

// Tests and verifies that a Ping Frame is correctly written with its flag
// permutations.
func (s) TestBridge_WritePing(t *testing.T) {
	wantData := [8]byte{1, 2, 3, 4, 5, 6, 7, 8}
	acks := []bool{true, false}

	for _, ack := range acks {
		t.Run(fmt.Sprintf("ack=%v", ack), func(t *testing.T) {
			c := &testConn{}
			f := NewFramerBridge(c, c, 0, nil)

			f.WritePing(ack, wantData)
			var wantFlags Flag
			if ack {
				wantFlags |= FlagPingAck
			}
			wantHdr := wantHeader{
				wantSize:      8,
				wantFrameType: FrameTypePing,
				wantFlag:      wantFlags,
				wantStreamID:  0,
			}
			if errs := checkWrittenHeader(c.wbuf[0:9], wantHdr); len(errs) > 0 {
				t.Errorf("WritePing():\n%s", errors.Join(errs...))
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
func (s) TestBridge_WriteGoAway(t *testing.T) {
	c := &testConn{}
	f := NewFramerBridge(c, c, 0, nil)
	f.WriteGoAway(2, ErrCodeFlowControl, []byte("debug_data"))

	wantHdr := wantHeader{
		wantSize:      18,
		wantFrameType: FrameTypeGoAway,
		wantFlag:      0,
		wantStreamID:  0,
	}
	if errs := checkWrittenHeader(c.wbuf[0:9], wantHdr); len(errs) > 0 {
		t.Errorf("WriteGoAway():\n%s", errors.Join(errs...))
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
func (s) TestBridge_WriteWindowUpdate(t *testing.T) {
	c := &testConn{}
	f := NewFramerBridge(c, c, 0, nil)
	f.WriteWindowUpdate(1, 2)

	wantHdr := wantHeader{
		wantSize:      4,
		wantFrameType: FrameTypeWindowUpdate,
		wantFlag:      0,
		wantStreamID:  1,
	}
	if errs := checkWrittenHeader(c.wbuf[0:9], wantHdr); len(errs) > 0 {
		t.Errorf("WriteWindowUpdate():\n%s", errors.Join(errs...))
	}
	if inc := readUint32(c.wbuf[9:13]); inc != 2 {
		t.Errorf("WriteWindowUpdate(): Inc: got %d, want %d", inc, 2)
	}
}

// Tests and verifies that a Continuation Frame is correctly written with its
// flag permutations.
func (s) TestBridge_WriteContinuation(t *testing.T) {
	wantData := "hdr block"
	endHeaders := []bool{true, false}

	for _, test := range endHeaders {
		t.Run(fmt.Sprintf("endHeaders=%v", test), func(t *testing.T) {
			c := &testConn{}
			f := NewFramerBridge(c, c, 0, nil)
			f.WriteContinuation(1, test, []byte("hdr block"))
			var wantFlags Flag
			if test {
				wantFlags |= FlagContinuationEndHeaders
			}
			wantHdr := wantHeader{
				wantSize:      len(wantData),
				wantFrameType: FrameTypeContinuation,
				wantFlag:      wantFlags,
				wantStreamID:  1,
			}
			if errs := checkWrittenHeader(c.wbuf[0:9], wantHdr); len(errs) > 0 {
				t.Errorf("WriteContinuation():\n%s", errors.Join(errs...))
			}
			if data := string(c.wbuf[9:]); data != wantData {
				t.Errorf("WriteContinuation(): Data: got %q, want %q", data, wantData)
			}
			c.wbuf = c.wbuf[:0]
		})

	}
}
