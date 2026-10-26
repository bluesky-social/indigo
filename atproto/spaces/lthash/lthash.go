package lthash

import (
	"crypto/sha256"
	"encoding/binary"
	"fmt"

	"lukechampine.com/blake3"
)

// Hash is an incremental LtHash state. Initialize it with New, Load, or Reset.
//
// Hash is not safe for concurrent use. Do not copy an initialized Hash: copies
// share its reusable BLAKE3 hasher. Use Load(h.Export()) for an independent hash
// with the same state.
type Hash struct {
	lanes  [2048]byte
	hasher *blake3.Hasher
	xofbuf [2048]byte
}

// New returns an empty Hash.
func New() Hash {
	return Hash{
		hasher: blake3.New(64, nil),
	}
}

// Load initializes a Hash from previously exported state.
func Load(state [2048]byte) Hash {
	h := New()
	h.lanes = state
	return h
}

// Export returns the current hash state (lanes).
func (h *Hash) Export() [2048]byte {
	return h.lanes
}

// Export2 copies the current hash state (lanes) into b.
// It panics unless b is exactly 2048 bytes long.
func (h *Hash) Export2(b []byte) {
	if len(b) != 2048 {
		panic("buffer size mismatch")
	}
	copy(b[:], h.lanes[:])
}

// Add adds an element to the state. Adding the same element twice is not a no-op.
func (h *Hash) Add(elem []byte) {
	if err := h.hashElem(elem); err != nil {
		panic(err)
	}
	for i := 0; i < 2048; i += 2 {
		lane := h.lanes[i : i+2]
		update := binary.LittleEndian.Uint16(lane) + binary.LittleEndian.Uint16(h.xofbuf[i:i+2])
		binary.LittleEndian.PutUint16(lane, update)
	}
}

// Remove subtracts an element from the state; it does not check membership.
func (h *Hash) Remove(elem []byte) {
	if err := h.hashElem(elem); err != nil {
		panic(err)
	}
	for i := 0; i < 2048; i += 2 {
		lane := h.lanes[i : i+2]
		update := binary.LittleEndian.Uint16(lane) - binary.LittleEndian.Uint16(h.xofbuf[i:i+2])
		binary.LittleEndian.PutUint16(lane, update)
	}
}

// Sum256 returns the SHA-256 digest of the current lane state.
func (h *Hash) Sum256() [32]byte {
	return sha256.Sum256(h.lanes[:])
}

// Reset clears the state while retaining the reusable hasher.
func (h *Hash) Reset() {
	clear(h.lanes[:])
	if h.hasher == nil {
		h.hasher = blake3.New(64, nil)
	}
}

// hashElem updates h.xofbuf with the BLAKE3 extended hash output (XOF) of elem.
//
// It returns an error, but is expected to never error if the element size is
// reasonable (for example, less than system memory).
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
