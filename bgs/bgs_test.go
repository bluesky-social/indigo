package bgs

import (
	"encoding/hex"
	"fmt"
	"testing"
)

type bitmapTest struct {
	bitmap   []byte
	bit      int
	expected bool
}

func (bt *bitmapTest) String() string {
	return fmt.Sprintf("%s,%d,%v", hex.EncodeToString(bt.bitmap), bt.bit, bt.expected)
}

func TestHasBit(t *testing.T) {
	tests := []bitmapTest{
		{[]byte{0x80, 0}, 0, true},
		{[]byte{0x80, 0}, 1, false},
		{[]byte{0x01, 0}, 7, true},
		{[]byte{0, 0x80}, 8, true},
		{[]byte{0, 1}, 15, true},
	}
	for _, bt := range tests {
		t.Run(bt.String(), func(st *testing.T) {
			actual := hasBit(bt.bitmap, bt.bit)
			if actual != bt.expected {
				st.Errorf("expected %v, got %v", bt.expected, actual)
			}
		})
	}
}

type hbmTest struct {
	bitmap   []byte
	bit      int
	basis    int
	expected bool
}

func (bt *hbmTest) String() string {
	return fmt.Sprintf("%s,%d,%d,%v", hex.EncodeToString(bt.bitmap), bt.bit, bt.basis, bt.expected)
}

func TestHeirarchicalBitMap(t *testing.T) {
	tests := []hbmTest{
		{[]byte{0x80, 0}, 0, 1024, true}, // every bit is expanded by 64
		{[]byte{0x80, 0}, 63, 1024, true},
		{[]byte{0x80, 0}, 64, 1024, false},
		{[]byte{0x80}, 63, 1024, true}, // every bit is expanded by 128
		{[]byte{0x80}, 64, 1024, true},
		{[]byte{0x80}, 127, 1024, true},
		{[]byte{0x80}, 128, 1024, false},
		{[]byte{0, 0, 0x80, 0}, 0, 1024, false}, // every bit is expanded by 32
		{[]byte{0, 0, 0x80, 0}, 512, 1024, true},
		{[]byte{0, 0, 0x80, 0}, 512 + 31, 1024, true},
		{[]byte{0, 0, 0x80, 0}, 512 + 32, 1024, false},
	}
	for _, bt := range tests {
		t.Run(bt.String(), func(st *testing.T) {
			actual := heirarchicalBitMap(bt.bitmap, bt.basis, bt.bit)
			if actual != bt.expected {
				st.Errorf("expected %v, got %v", bt.expected, actual)
			}
		})
	}
}
