package bgs

import (
	"encoding/hex"
	"errors"
	"fmt"
	"hash"
	"hash/fnv"
)

type ShardHashMatcher struct {
	hasher hash.Hash32
	shards []byte
	mask   uint32
	basis  int
}

var ErrBadBasis = errors.New("basis must be power of two")

// Create a new heirarchical bitmap matcher
// shards is a hex string, "0f" would match the second half of the shard space, identically so would "00ff".
// basis is the maximum number of shards and must be a power of two. 1024 is probably a good default.
func NewShardHashMatcher(shards string, basis int) (*ShardHashMatcher, error) {
	if shards == "" {
		return nil, nil
	}
	if !isPowerOfTwo(basis) {
		return nil, ErrBadBasis
	}
	shbytes, err := parseShardsStr(shards)
	if err != nil {
		return nil, err
	}
	mask := uint64(0x0ffffffff)
	for mask > uint64(basis) {
		mask = mask >> 1
	}
	return &ShardHashMatcher{
		hasher: fnv.New32a(),
		shards: shbytes,
		mask:   uint32(mask),
		basis:  basis,
	}, nil
}

// hash the input and see if it matches with the filter pattern defined in NewShardHashMatcher
func (shm *ShardHashMatcher) Match(blob []byte) bool {
	shm.hasher.Reset()
	shm.hasher.Write(blob)
	hashi := int(shm.hasher.Sum32() & shm.mask)
	return heirarchicalBitMap(shm.shards, shm.basis, hashi)
}

// TODO: there's a clever way to do this like (((x - 1) & x) == 0) ?
func isPowerOfTwo(x int) bool {
	val := 1
	for {
		if val == x {
			return true
		}
		if val > x {
			return false
		}
		nval := val << 1
		if nval < val {
			return false
		}
		val = nval
	}
}

var ErrBadShardLength = errors.New("shards decoded byte length must be a power of 2")

func parseShardsStr(shards string) ([]byte, error) {
	if len(shards) == 0 {
		return nil, nil
	}
	xb, err := hex.DecodeString(shards)
	if err != nil {
		return nil, fmt.Errorf("bad shards, %w", err)
	}
	if !isPowerOfTwo(len(xb)) {
		return nil, ErrBadShardLength
	}
	return xb, nil
}

func hasBit(bitmap []byte, bit int) bool {
	byteIndex := bit / 8
	bitIndex := bit % 8
	return (bitmap[byteIndex] & (0x80 >> bitIndex)) != 0
}

// for some bitmap with power-of-two bits,
func heirarchicalBitMap(bitmap []byte, basis, bit int) bool {
	bitmapNBits := len(bitmap) * 8
	for basis > bitmapNBits {
		basis /= 2
		bit /= 2
	}
	return hasBit(bitmap, bit)
}
