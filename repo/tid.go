package repo

import (
	"math/rand"
	"time"
)

const alpha = "234567abcdefghijklmnopqrstuvwxyz"

func s32encode(i uint64) string {
	var s string
	for i > 0 {
		c := i & 0x1f
		i = i >> 5
		s = alpha[c:c+1] + s
	}
	return s
}

func NextTID() string {
	t := time.Now()

	clockid := uint64(rand.Uint32() & 0x1f)

	return s32encode(uint64(t.UnixMilli())) + s32encode(clockid)
}
