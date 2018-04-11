package msgdecoder

import (
	"encoding/binary"
)

// RecvMsg is a message constructed from the incoming
// bytes on the transport for a stream.
// An instance of RecvMsg will contain only one of the
// following: message header related fields, data slice
// or error.
type RecvMsg struct {
	// Following three are message header related
	// fields.
	// true if the message was compressed by the other
	// side.
	IsCompressed bool
	// WindowUpdate to be sent out for this message.
	WindowUpdate uint32

	// Data payload of the message.
	Data []byte

	// Err occured while reading.
	// nil: received some data
	// io.EOF: stream is completed. data is nil.
	// other non-nil error: transport failure. data is nil.
	Err error

	Next *RecvMsg
}

// gRPC Messages:
// First 5 bytes is the message header:
//   First byte: Payload format.
//   Next 4 bytes: Length of the message.
// Rest of the bytes is the message payload.
//
// TODO(mmukhi): Write unit tests.
type MessageDecoder struct {
	// current message being read by the transport.
	current  *RecvMsg
	dataOfst int
	padding  int
	// hdr stores the message header as it is beind received by the transport.
	hdr     []byte
	hdrOfst int
	// Callback used to send decoded messages.
	dispatch func(*RecvMsg)
}

func NewMessageDecoder(dispatch func(*RecvMsg)) *MessageDecoder {
	return &MessageDecoder{
		hdr:      make([]byte, 5),
		dispatch: dispatch,
	}
}

func (m *MessageDecoder) Decode(b []byte, padding int) {
	m.padding += padding
	for len(b) > 0 {
		// Case 1: A complete message hdr was received earlier.
		if m.current != nil {
			n := copy(m.current.Data[m.dataOfst:], b)
			m.dataOfst += n
			b = b[n:]
			if m.dataOfst == len(m.current.Data) { // Message is complete.
				m.dispatch(m.current)
				m.current = nil
				m.dataOfst = 0
			}
			continue
		}
		// Case 2a: No message header has been received yet.
		if m.hdrOfst == 0 {
			// case 2a.1: b has the whole header
			if len(b) >= 5 {
				m.parseHeader(b[:5])
				b = b[5:]
				continue
			}
			// case 2a.2: b has partial header
			n := copy(m.hdr, b)
			m.hdrOfst = n
			b = b[n:]
			continue
		}
		// Case 2b: Partial message header was received earlier.
		n := copy(m.hdr[m.hdrOfst:], b)
		m.hdrOfst += n
		b = b[n:]
		if m.hdrOfst == 5 { // hdr is complete.
			m.hdrOfst = 0
			m.parseHeader(m.hdr)
		}
	}
}

func (m *MessageDecoder) parseHeader(b []byte) {
	buf := getMem(b[1:5])
	hdr := &RecvMsg{
		IsCompressed: int(b[0]) == 1,
		WindowUpdate: uint32(len(buf) + m.padding + 5),
	}
	m.padding = 0
	// Dispatch the information retreived from message header so
	// that the RPC goroutine can send a proactive window update as we
	// wait for the rest of it.
	m.dispatch(hdr)
	m.current = &RecvMsg{
		Data: buf,
	}
	if len(buf) == 0 {
		m.dispatch(m.current)
		m.current = nil
		m.dataOfst = 0
	}
}

func getMem(l []byte) []byte {
	length := binary.BigEndian.Uint32(l)
	// TODO(mmukhi): Reuse this memory.
	return make([]byte, length)
}

func GetMessageHeader(l int, isCompressed bool) []byte {
	// TODO(mmukhi): Investigate if this memory is worth
	// reusing.
	hdr := make([]byte, 5)
	if isCompressed {
		hdr[0] = byte(1)
	}
	binary.BigEndian.PutUint32(hdr[1:], uint32(l))
	return hdr
}
