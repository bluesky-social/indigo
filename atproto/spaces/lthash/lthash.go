package lthash

import (
	"crypto/sha256"
	"encoding/binary"
	"fmt"

	"lukechampine.com/blake3"
)

// NOTE: this struct is *not* concurrency safe. Multiple concurrent mutations (additions, deletions) can race on shared buffers and corrupt the state. Calling code must use a mutex to serialize updates if concurrency is needed.
type Hash struct {
	lanes  [2048]byte
	hasher *blake3.Hasher
	xofbuf [2048]byte
}

func New() Hash {
	return Hash{
		// NOTE: the hash size argument to this initializer seems to be arbitrary because we are using XOF mode
		hasher: blake3.New(64, nil),
	}
}

// Initializes a [Hash] from existing (exported) state.
func Load(state *[2048]byte) Hash {
	h := New()
	copy(h.lanes[:], state[:])
	return h
}

// Copies the current hash state (lanes) to the target buffer. Calling code must allocate an appropriately sized buffer ahead of time.
func (h *Hash) Export(buf *[2048]byte) {
	copy(buf[:], h.lanes[:])
}

func (h *Hash) Add(elem []byte) error {
	if err := h.hashElem(elem); err != nil {
		return err
	}
	for i := 0; i < 2048; i += 2 {
		lane := h.lanes[i : i+2]
		update := binary.LittleEndian.Uint16(lane) + binary.LittleEndian.Uint16(h.xofbuf[i:i+2])
		binary.LittleEndian.PutUint16(lane, update)
	}
	return nil
}

func (h *Hash) Remove(elem []byte) error {
	if err := h.hashElem(elem); err != nil {
		return err
	}
	for i := 0; i < 2048; i += 2 {
		lane := h.lanes[i : i+2]
		update := binary.LittleEndian.Uint16(lane) - binary.LittleEndian.Uint16(h.xofbuf[i:i+2])
		binary.LittleEndian.PutUint16(lane, update)
	}
	return nil
}

// Returns the SHA-256 (256 bit output) hash of the current lane state, as bytes.
func (h *Hash) Sum256() [32]byte {
	return sha256.Sum256(h.lanes[:])
}

// Updates h.xofbuf with the blake3 extended hash output (XOF) of the input element bytes.
//
// This method does return an error, but is expected to never error if the element size is reasonable (eg, less than system memory).
func (h *Hash) hashElem(elem []byte) error {
	h.hasher.Reset()
	n, err := h.hasher.Write(elem)
	if err != nil {
		return err
	}
	if n != len(elem) {
		return fmt.Errorf("LtHash blake3 XOF hashing did not consume all element bytes (%d != %d)", n, len(elem))
	}
	reader := h.hasher.XOF()
	n, err = reader.Read(h.xofbuf[:])
	if err != nil {
		return err
	}
	if n != len(h.xofbuf) {
		return fmt.Errorf("LtHash blake3 XOF hashing did not return all buffer bytes (%d != %d)", n, len(h.xofbuf))
	}
	return nil
}
