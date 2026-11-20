/*
star is a simple package for reading and writing DASL STAR archives (as used in atproto).

The STAR file format is a simple container for bytestring key/value pairs, with an additional DASL CBOR metadata header. The metadata object header has a required '$type' string field which indicates what sub-type the STAR file is (eg, what kind of data is enclosed, and any semantics or rules about key/value pairs).

Keys can be any bytestring, though are often UTF-8 encoded strings. Values are also bytestrings, and are often CBOR objects (with DASL CBOR encoding rules).

Read more about STAR files: https://dasl.ing/star.html

# Reading STAR Files

[Reader] provides a scanner API similar to [bufio.Scanner] that minimizes allocations:

	reader := star.NewReader(f)

	// the metadata header is cached and can be read at any time
	meta, err := reader.Meta()

	if err != nil {
		return err
	}

	for reader.Scan() {
	    // Key() and Value() return slices in to buffers that get overwritten by the next pair
	    processPair(reader.Key(), reader.Value())

		// make a copy if you need to keep keys or values
	    storeSlice(slices.Clone(reader.Value()))
	}

	if err := reader.Err(); err != nil {
		return err
	}

The [Reader.ReadPair] method is an alternative direct API for reading pairs.

# Writing STAR Files

[Writer] requires the metadata object to be written before any pairs, and [Writer.Close] must be called to write a terminating marker and flush buffers:

	writer := star.NewWriter(f)
	// write the header metadata first. MUST be CBOR bytes with a '$type' field
	if err := writer.WriteMeta(metaBytes); err != nil {
		return err
	}

	// write simple key/value pairs
	if err = writer.WritePair([]byte("some/key"), []byte("lorem ipsum")); err != nil {
		return err
	}

	// write mandatory terminal marker
	if err := writer.Close(); err != nil {
		return err
	}

The [Writer.MarshalMeta] and [Writer.MarshalPairCBOR] variants help with CBOR encoding.

# Limitations

The current API is designed for reading and writing pairs (keys and values) which easily fit in memory (via buffer allocation). Streaming reads of individual values is not supported.

Both reading and writing are designed for single-threaded use cases, and do not implement internal locking.

[Reader] does not verify that varint length prefixes are in canonical encoding.

[Writer] does not verify the metadata header object.
*/
package star
