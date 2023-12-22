package util

func Must(err error) {
	if err != nil {
		panic(err)
	}
}

func MustResult[T any](v T, err error) T {
	if err != nil {
		panic(err)
	}
	return v
}
