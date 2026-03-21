package common

import (
	"bytes"
	"io"
)

// ReaderAtWrapper wraps an io.Reader to provide io.ReaderAt support by buffering in memory.
// It also exposes Size() to return the total read length.
type ReaderAtWrapper struct {
	io.Reader
	buf *bytes.Reader
}

func (r *ReaderAtWrapper) ReadAt(p []byte, off int64) (n int, err error) {
	if r.buf == nil {
		b, err := io.ReadAll(r.Reader)
		if err != nil {
			return 0, err
		}
		r.buf = bytes.NewReader(b)
	}
	return r.buf.ReadAt(p, off)
}

func (r *ReaderAtWrapper) Size() int64 {
	if r.buf == nil {
		b, err := io.ReadAll(r.Reader)
		if err != nil {
			// If we can't read the body, we can't determine the size.
			// Return 0 so the caller handles the failure during ReadAt.
			return 0
		}
		r.buf = bytes.NewReader(b)
	}
	return r.buf.Size()
}
