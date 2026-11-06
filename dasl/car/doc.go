/*
car is a simple package for reading and writing DASL CAR archives (as used in atproto).

The CAR file format is a simple container for binary data chunks ("blocks"), prefixed by a content hash (CID). A small header enumerates a set of "root" blocks by CID.

This package implements only the DASL CAR specification, which is locked down to specific CID types.

Read more about CAR files: https://dasl.ing/car.html

# Reading CAR Files

[Reader] provides a scanner API similar to [bufio.Scanner] that minimizes allocations:

	reader := car.NewReader(f)

	// the header is cached and can be read at any time
	hdr, err := reader.Header()
	if err != nil {
		return err
	}

	for reader.Scan() {
		// Data() returns a pointer to an internal buffer that get overwritten by the next block
		processBlock(reader.CID(), reader.Data())

		// make a copy if you need to keep block data
		storeSlice(slices.Clone(reader.Data()))
	}
	if err := reader.Err(); err != nil {
		return err
	}

The [Reader.ReadBlock] method is an alternative direct API for reading blocks.

# Writing CAR Files

[Writer] requires the header to be written before any blocks, and [Writer.Flush] must be called at the end to flush buffers:

	writer := car.NewWriter(f)
	// write the header metadata first
	if err := writer.WriteHeader([]cid.Cid{rootCID}); err != nil {
		return err
	}

	// write simple bytestring block (raw CID type)
	data := []byte("lorem ipsum")
	if err = writer.WriteBlock(cid.HashBytes(data), data); err != nil {
		return err
	}

	// mandatory flush
	if err := writer.Flush(); err != nil {
		return err
	}

The [Writer.MarshalRootCBOR] and [Writer.MarshalBlockCBOR] variants help with CBOR encoding and CID computation.

# Limitations

The current API is designed for reading and writing blocks which easily fit in memory (via buffer allocation). Streaming reads of large blocks is not supported.

This package does not support additional metadata fields in the header, as used by MASL.

Both reading and writing are designed for single-threaded use cases, and do not implement internal locking.

[Writer] does not verify the metadata header object.
*/
package car
