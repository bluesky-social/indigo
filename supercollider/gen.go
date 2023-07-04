package supercollider

import (
	"fmt"
	"math/rand"
	"time"

	petname "github.com/dustinkirkland/golang-petname"
)

// GenHandles generates a list of random handles for the given prefix using petnames
func GenHandles(suffix string, n int) []string {
	rand.Seed(time.Now().UnixNano())
	handles := make([]string, 0)

	for i := 0; i < n; i++ {
		pn := petname.Generate(3, "-")
		handles = append(handles, fmt.Sprintf("%s-%d.%s", pn, i, suffix))
	}

	return handles
}
