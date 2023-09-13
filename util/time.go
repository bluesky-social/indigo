package util

import (
	"fmt"
	"time"
)

const ISO8601 = "2006-01-02T15:04:05.000Z"

const ISO8601_milli = "2006-01-02T15:04:05.000000Z"

const ISO8601_numtz = "2006-01-02T15:04:05.000-07:00"

const ISO8601_numtz_milli = "2006-01-02T15:04:05.000000-07:00"

const ISO8601_sec = "2006-01-02T15:04:05Z"

const ISO8601_numtz_sec = "2006-01-02T15:04:05-07:00"

func ParseTimestamp(s string) (time.Time, error) {
	t, err := time.Parse(ISO8601, s)
	if err == nil {
		return t, nil
	}

	t, err = time.Parse(ISO8601_milli, s)
	if err == nil {
		return t, nil
	}

	t, err = time.Parse(ISO8601_numtz, s)
	if err == nil {
		return t, nil
	}

	t, err = time.Parse(ISO8601_numtz_milli, s)
	if err == nil {
		return t, nil
	}

	t, err = time.Parse(ISO8601_sec, s)
	if err == nil {
		return t, nil
	}

	t, err = time.Parse(ISO8601_numtz_sec, s)
	if err == nil {
		return t, nil
	}

	return time.Time{}, fmt.Errorf("failed to parse %q as timestamp", s)
}
