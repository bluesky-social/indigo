package star

const (
	// The 4-byte binary file header.
	MagicHeader uint32 = 0x2A6C0100

	// Single-byte marker indicating that a pair follows.
	PairMarker byte = 0x01
	// Single-byte marker indicating end of the STAR file.
	EndMarker byte = 0x00

	MediaType     string = "application/vnd.dasl.star"
	FileExtension string = ".star"

	defaultMaxMetaSize  uint64 = 5 * 1024 * 1024
	defaultMaxKeySize   uint64 = 8 * 1024
	defaultMaxValueSize uint64 = 5 * 1024 * 1024
)

// helper for parsing out just the '$type' struct without unnecessary additional allocations
type metaType struct {
	// this is a ptr to catch both "missing field" or "nil value" situations
	Type *string `cbor:"$type" json:"$type"`
}
