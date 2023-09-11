package backfill

import (
	"io"

	"github.com/prometheus/client_golang/prometheus"
)

type instrumentedReader struct {
	source  io.ReadCloser
	counter prometheus.Counter
}

func (r instrumentedReader) Read(b []byte) (int, error) {
	n, err := r.source.Read(b)
	r.counter.Add(float64(n))
	return n, err
}

func (r instrumentedReader) Close() error {
	var buf [32]byte
	var n int
	var err error
	for err == nil {
		n, err = r.source.Read(buf[:])
		r.counter.Add(float64(n))
	}
	closeerr := r.source.Close()
	if err != nil && err != io.EOF {
		return err
	}
	return closeerr
}
