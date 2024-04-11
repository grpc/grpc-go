package encoding

import (
	"io"
	"sync/atomic"
)

type BufferSlice []*Buffer

type Buffer struct {
	data []byte
	refs atomic.Int32
	free func([]byte)
}

type BufferProvider interface {
	GetAndSetBuffer(length int, write func([]byte)) *Buffer
}

type BufferProviderFunc func(length int, write func(b []byte)) *Buffer

func (b BufferProviderFunc) GetAndSetBuffer(length int, write func([]byte)) *Buffer {
	return b(length, write)
}

var NoopBufferProvider BufferProvider = BufferProviderFunc(func(length int, write func(b []byte)) *Buffer {
	buf := make([]byte, length)
	write(buf)
	return NewBuffer(buf, nil)
})

func NewBuffer(data []byte, free func([]byte)) *Buffer {
	return (&Buffer{data: data, free: free}).Ref()
}

func (b *Buffer) ReadOnlyData() []byte {
	return b.data
}

func (b *Buffer) Ref() *Buffer {
	b.refs.Add(1)
	return b
}

func (b *Buffer) Free() {
	if b.refs.Add(-1) == 0 && b.free != nil {
		b.free(b.data)
		b.data = nil
	}
}

type Writer struct {
	buffers  *BufferSlice
	provider BufferProvider
}

func (s *Writer) Write(p []byte) (n int, err error) {
	*s.buffers = append(*s.buffers, s.provider.GetAndSetBuffer(len(p), func(b []byte) { copy(b, p) }))

	return len(p), nil
}

func NewWriter(buffers *BufferSlice, provider BufferProvider) *Writer {
	return &Writer{buffers: buffers, provider: provider}
}

type Reader struct {
	data BufferSlice
	len  int
	idx  int
}

func (r *Reader) Len() int {
	return r.len
}

func (r *Reader) Read(buf []byte) (n int, _ error) {
	for len(buf) != 0 && r.len != 0 {
		data := r.data[0].ReadOnlyData()
		copied := copy(buf, data[r.idx:])
		r.len -= copied

		buf = buf[copied:]

		if copied == len(data) {
			r.data = r.data[1:]
			r.idx = 0
		} else {
			r.idx += copied
		}
		n += copied
	}

	if n == 0 {
		return 0, io.EOF
	}

	return n, nil
}

func (s BufferSlice) Reader() *Reader {
	return &Reader{
		data: s,
		len:  s.Len(),
	}
}

func (s BufferSlice) Len() (length int) {
	for _, b := range s {
		length += len(b.ReadOnlyData())
	}
	return length
}

func (s BufferSlice) Ref() {
	for _, b := range s {
		b.Ref()
	}
}

func (s BufferSlice) Free() {
	for _, b := range s {
		b.Free()
	}
}

func (s BufferSlice) WriteTo(out []byte) {
	out = out[:0]
	for _, b := range s {
		out = append(out, b.ReadOnlyData()...)
	}
}

func (s BufferSlice) Materialize() []byte {
	out := make([]byte, s.Len())
	s.WriteTo(out)
	return out
}

func (s BufferSlice) LazyMaterialize(provider BufferProvider) *Buffer {
	if len(s) == 1 {
		return s[0].Ref()
	}
	return provider.GetAndSetBuffer(s.Len(), s.WriteTo)
}
