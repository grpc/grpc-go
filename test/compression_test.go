package test

import (
	"io"

	"google.golang.org/grpc/encoding"
)

const testCompressorName = "testCompressor"

type testCompressor struct {
	name string
}

func init() {
	c := &testCompressor{
		name: testCompressorName,
	}
	encoding.RegisterCompressor(c)
}

type customWriterCloser struct {
	io.Writer
}

func (d *customWriterCloser) Close() error {
	return nil
}

func (c *testCompressor) Compress(w io.Writer) (io.WriteCloser, error) {
	customCloser := &customWriterCloser{w}
	return customCloser, nil
}

func (c *testCompressor) Decompress(r io.Reader) (io.Reader, error) {
	return r, nil
}

func (c *testCompressor) Name() string {
	return testCompressorName
}
