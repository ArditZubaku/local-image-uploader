package storage

import (
	"io"
	"sync"
)

// bufSize is deliberately far above io.Copy's 32 KiB default. On a LAN
// transfer the per-syscall cost, not bandwidth, is what caps throughput,
// and these copies always sit between a socket and a disk.
const bufSize = 1 << 20

var bufPool = sync.Pool{
	New: func() any {
		b := make([]byte, bufSize)
		return &b
	},
}

// copyBuffered is io.Copy with a pooled 1 MiB buffer, bypassing the
// ReaderFrom/WriterTo fast paths (neither applies to a zip or multipart
// stream, and both would silently fall back to a 32 KiB buffer).
func copyBuffered(dst io.Writer, src io.Reader) (int64, error) {
	bp := bufPool.Get().(*[]byte)
	defer bufPool.Put(bp)
	return copyWith(dst, src, *bp)
}

// copyExactly copies exactly n bytes from src, zero-padding if src ends
// early. A file that shrinks between planning and streaming would
// otherwise desync a response whose Content-Length is already sent.
func copyExactly(dst io.Writer, src io.Reader, n int64) error {
	bp := bufPool.Get().(*[]byte)
	defer bufPool.Put(bp)
	buf := *bp

	written, err := copyWith(dst, io.LimitReader(src, n), buf)
	if err != nil {
		return err
	}

	for written < n {
		chunk := buf[:min(int64(len(buf)), n-written)]
		clear(chunk)
		w, err := dst.Write(chunk)
		written += int64(w)
		if err != nil {
			return err
		}
	}

	return nil
}

func copyWith(dst io.Writer, src io.Reader, buf []byte) (int64, error) {
	var written int64
	for {
		nr, rerr := src.Read(buf)
		if nr > 0 {
			nw, werr := dst.Write(buf[:nr])
			written += int64(nw)
			if werr != nil {
				return written, werr
			}
			if nw != nr {
				return written, io.ErrShortWrite
			}
		}
		if rerr == io.EOF {
			return written, nil
		}
		if rerr != nil {
			return written, rerr
		}
	}
}
