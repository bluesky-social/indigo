package star

import (
	"fmt"
	"os"
	"slices"
)

func ExampleWriter() {

	// open a new file on disk for writing
	f, err := os.Create("/tmp/example.star")
	if err != nil {
		panic(err)
	}
	defer f.Close()

	// create the STAR writer
	writer := NewWriter(f)

	// write a 'meta' header object. this MUST include a '$type' string field, and MUST be written before writing any key/value pairs
	meta := map[string]any{
		"$type":  "example-star-type",
		"number": uint64(123),
		"string": "one-two-three",
	}
	if err := writer.MarshalMeta(meta); err != nil {
		panic(err)
	}

	// write simple string key/value pair
	if err = writer.WritePair([]byte("some/key"), []byte("lorem ipsum")); err != nil {
		panic(err)
	}

	// serialize a value struct as CBOR
	value := struct {
		Name string `cbor:"name"`
		Age  uint64 `cbor:"age"`
	}{
		Name: "Riley",
		Age:  85,
	}
	if err := writer.MarshalPairCBOR([]byte("account/riley"), &value); err != nil {
		panic(err)
	}

	// write terminating marker and flush output
	if err := writer.Close(); err != nil {
		panic(err)
	}

	// Output:
}

func ExampleReader() {

	// open an existing file from disk
	f, err := os.Open("testdata/example.star")
	if err != nil {
		panic(err)
	}
	defer f.Close()

	// create a reader (this doesn't do anything yet)
	reader := NewReader(f)

	// check the type of this STAR file.
	// this reads and validates the meta header object if needed
	metaType, err := reader.MetaType()
	if err != nil {
		panic(err)
	}
	if metaType != "example-star-type" {
		panic("unexpected STAR file type")
	}

	// parse the meta object (this could be a struct as well)
	var meta map[string]any
	if err := reader.UnmarshalMeta(&meta); err != nil {
		panic(err)
	}

	// scan over all pairs, similar to bufio.Scanner
	data := map[string]any{}
	for reader.Scan() {
		// check that key bytes are valid UTF-8 string
		key, err := reader.KeyString()
		if err != nil {
			panic(err)
		}
		fmt.Printf("%s: len(val) = %d\n", key, len(reader.Value()))

		// Key() and Value() return slices in to buffers that get overwritten by the next pair. make copies to retain data
		data[key] = slices.Clone(reader.Value())
	}
	if reader.Err() != nil {
		panic(reader.Err())
	}
	// Output:
	// some/key: len(val) = 11
	// account/riley: len(val) = 18
}
