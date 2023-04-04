package labeler

import (
	"fmt"
	"reflect"
	"testing"
)

func TestDedupeStrings(t *testing.T) {

	testCases := []struct {
		in  []string
		out []string
	}{
		{in: []string{"a", "b", "c"}, out: []string{"a", "b", "c"}},
		{in: []string{"a", "a", "a"}, out: []string{"a"}},
		{in: []string{"a", "b", "a"}, out: []string{"a", "b"}},
	}

	for _, c := range testCases {
		res := dedupeStrings(c.in)
		if !reflect.DeepEqual(res, c.out) {
			t.Log(fmt.Sprintf("strings expected:%s got:%s", c.out, res))
			t.Fail()
		}
	}
}
