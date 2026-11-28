package lthash

import (
	"crypto/sha256"
	"encoding/binary"
	"fmt"
	"sync"

	"lukechampine.com/blake3"
)

// The size in bytes of [Hash] binary state representation.
const StateSize = 2048

// this argument to the blake3.New() initializer seems to be arbitrary because we are using XOF mode
const blake3Size = 64

var hasherPool = sync.Pool{
	New: func() any {
		return blake3.New(blake3Size, nil)
	},
}

// Hash maintains the incremental LtHash state for a multiset of byte elements
// using 1024 little-endian 16-bit lanes and BLAKE3 XOF.
//
// Hash is not safe for concurrent use; callers must synchronize concurrent
// reads and mutations externally.
type Hash struct {
	lanes [2048]byte
}

// New initializes an empty [Hash].
func New() *Hash {
	return &Hash{}
}

// Load initializes a [Hash] from existing (exported) state.
func Load(state [2048]byte) *Hash {
	h := New()
	h.lanes = state
	return h
}

// Bytes exports the current hash state (lanes).
func (h *Hash) Bytes() [2048]byte {
	return h.lanes
}

// UnmarshalBinary implements the [encoding.BinaryUnmarshaler] interface.
func (h *Hash) UnmarshalBinary(b []byte) error {
	if len(b) != StateSize {
		return fmt.Errorf("wrong binary size for lthash.Hash state: %d != %d", len(b), StateSize)
	}
	copy(h.lanes[:], b[0:2048])
	return nil
}

// MarshalBinary implements the [encoding.BinaryMarshaler] interface using [Hash.Bytes]. It never returns an error.
func (h Hash) MarshalBinary() ([]byte, error) {
	b := h.Bytes()
	return b[:], nil
}

// Add incorporates elem into the hash state.
// Adding the same element multiple times accumulates each occurrence.
func (h *Hash) Add(elem []byte) {
	var xofbuf [2048]byte
	hashElem(elem, &xofbuf)
	for i := 0; i < 2048; i += 2 {
		lane := h.lanes[i : i+2]
		update := binary.LittleEndian.Uint16(lane) + binary.LittleEndian.Uint16(xofbuf[i:i+2])
		binary.LittleEndian.PutUint16(lane, update)
	}
}

// Remove removes elem from the hash state, reversing a prior [Hash.Add].
// It does not track membership; removing an element that was never added still mutates the state.
func (h *Hash) Remove(elem []byte) {
	var xofbuf [2048]byte
	hashElem(elem, &xofbuf)
	for i := 0; i < 2048; i += 2 {
		lane := h.lanes[i : i+2]
		update := binary.LittleEndian.Uint16(lane) - binary.LittleEndian.Uint16(xofbuf[i:i+2])
		binary.LittleEndian.PutUint16(lane, update)
	}
}

// Sum256 returns the SHA-256 (256 bit output) hash of the current lane state as bytes.
func (h *Hash) Sum256() [32]byte {
	return sha256.Sum256(h.lanes[:])
}

// Updates xofbuf with the blake3 extended hash output (XOF) of the input element bytes.
func hashElem(elem []byte, xofbuf *[2048]byte) {

	hasher := hasherPool.Get().(*blake3.Hasher)

	hasher.Reset()

	// safe to ignore Write() return values here: the hash.Hash interface requires that Write (io.Writer) never returns an error, and always returns n as the size of input
	hasher.Write(elem)

	// safe to ignore Read() return values here: xof.Read() is guaranteed to succeed up to 2^64 bytes, and we are reading only a couple kilobytes
	// The blake3 implementation internally uses parallelization (WaitGroup) for XOF reads over 1024 bytes. That is inefficient for small reads, so do two partial reads instead.
	xof := hasher.XOF()
	xof.Read(xofbuf[0:1024])
	xof.Read(xofbuf[1024:2048])

	hasherPool.Put(hasher)
}
