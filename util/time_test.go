package util

import "testing"

func TestTimeParsing(t *testing.T) {
	good := []string{
		"2023-07-19T21:54:14.165300Z",
		"2023-07-19T21:54:14.163Z",
		"2023-07-19T21:52:02.000+00:00",
		"2023-07-19T21:52:02.123456+00:00",
		"2023-09-13T11:23:33+09:00",
	}

	for _, g := range good {
		_, err := ParseTimestamp(g)
		if err != nil {
			t.Fatal(err)
		}
	}
}
