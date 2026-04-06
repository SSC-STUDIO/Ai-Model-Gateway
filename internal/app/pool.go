package app

import (
	"bytes"
	"sync"
)

const (
	// bufferSize is the default size for pooled buffers (32KB)
	bufferSize = 32 * 1024
)

var (
	// bodyBufferPool pools byte slices for request body reading
	bodyBufferPool = sync.Pool{
		New: func() interface{} {
			buf := make([]byte, bufferSize)
			return &buf
		},
	}

	// responseBufferPool pools bytes.Buffer for response building
	responseBufferPool = sync.Pool{
		New: func() interface{} {
			return new(bytes.Buffer)
		},
	}
)

// GetBodyBuffer gets a byte slice from the pool
func GetBodyBuffer() []byte {
	bufPtr := bodyBufferPool.Get().(*[]byte)
	return *bufPtr
}

// PutBodyBuffer returns a byte slice to the pool
func PutBodyBuffer(buf []byte) {
	if cap(buf) < bufferSize {
		return // Don't put small buffers back
	}
	bodyBufferPool.Put(&buf)
}

// GetResponseBuffer gets a bytes.Buffer from the pool
func GetResponseBuffer() *bytes.Buffer {
	buf := responseBufferPool.Get().(*bytes.Buffer)
	buf.Reset()
	return buf
}

// PutResponseBuffer returns a bytes.Buffer to the pool
func PutResponseBuffer(buf *bytes.Buffer) {
	if buf != nil {
		responseBufferPool.Put(buf)
	}
}
