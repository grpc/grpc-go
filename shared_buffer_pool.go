package grpc

// SharedBufferPool is a pool of buffers that can be shared.
type SharedBufferPool interface {
	Get(length int) []byte
	Put(*[]byte)
}
