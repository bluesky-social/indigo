package carstore

import (
	"encoding/binary"
	"io"
)

// Length-delimited Write
// Writer stream gets Uvarint length then concatenated data
func LdWrite(w io.Writer, d ...[]byte) (int64, error) {
	var sum uint64
	for _, s := range d {
		sum += uint64(len(s))
	}

	buf := make([]byte, 8)
	n := binary.PutUvarint(buf, sum)
	nw, err := w.Write(buf[:n])
	if err != nil {
		return 0, err
	}

	for _, s := range d {
		onw, err := w.Write(s)
		if err != nil {
			return int64(nw), err
		}
		nw += onw
	}

	return int64(nw), nil
}
