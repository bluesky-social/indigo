package repo

import (
	"math/rand"
	"sync"
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

func init() {
	clockId = uint64(rand.Int() & 0x1f)
}

var lastTime uint64
var clockId uint64
var ltLock sync.Mutex

func NextTID() string {
	t := uint64(time.Now().UnixMicro())

	ltLock.Lock()
	if lastTime >= t {
		t = lastTime + 1
	}

	lastTime = t
	ltLock.Unlock()

	return s32encode(uint64(t)) + s32encode(clockId)
}
