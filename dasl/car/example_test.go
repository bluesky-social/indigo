package car

import (
	"fmt"
	"os"
	"slices"

	"github.com/hyphacoop/go-dasl/cid"
)

func ExampleWriter() {

	// open a new file on disk for writing
	f, err := os.Create("/tmp/example.car")
	if err != nil {
		panic(err)
	}
	defer f.Close()

	// create the CAR writer
	writer := NewWriter(f)

	// write root block
	meta := map[string]any{
		"what":   "example metadata block referenced from header roots",
		"number": 123,
	}
	if err := writer.MarshalRootCBOR(meta); err != nil {
		panic(err)
	}

	// write simple string block as raw CID type
	bstr := []byte("lorem ipsum")
	c := cid.HashBytes(bstr)
	if err = writer.WriteBlock(c, bstr); err != nil {
		panic(err)
	}

	// serialize a struct as DASL CBOR
	value := struct {
		Name string `cbor:"name"`
		Age  uint64 `cbor:"age"`
	}{
		Name: "Riley",
		Age:  85,
	}
	if err := writer.MarshalBlockCBOR(&value); err != nil {
		panic(err)
	}

	// flush output
	if err := writer.Flush(); err != nil {
		panic(err)
	}

	// Output:
}

func ExampleReader() {

	// open an existing file from disk
	f, err := os.Open("testdata/example.car")
	if err != nil {
		panic(err)
	}
	defer f.Close()

	// create a reader (this doesn't do anything yet)
	reader := NewReader(f)

	// read header root CID
	hdr, err := reader.Header()
	if err != nil {
		panic(err)
	}
	fmt.Println(hdr.Roots[0].String())

	// scan over all pairs, similar to bufio.Scanner
	data := map[string]any{}
	for reader.Scan() {
		c := reader.CID()
		fmt.Printf("%s: len(block) = %d\n", c.String(), len(reader.Data()))

		// CID() and Data() return slices in to buffers that get overwritten by the next pair. make copies to retain data
		data[c.String()] = slices.Clone(reader.Data())
	}
	if reader.Err() != nil {
		panic(reader.Err())
	}
	// Output:
	// bafyreib6osx3doujf2dyvs57sfrrpo4fegfru4ikcvr3gv46qvrfwgdtim
	// bafyreib6osx3doujf2dyvs57sfrrpo4fegfru4ikcvr3gv46qvrfwgdtim: len(block) = 68
	// bafkreic6fp2x2p2ays3n62o26gjwzn3g7azdos2pyaszu7f76bxc64hsne: len(block) = 11
	// bafyreiej4n5vgx6yc2unzm74pxfmestocbv3rewst73rg6fia3r277wef4: len(block) = 18
}
