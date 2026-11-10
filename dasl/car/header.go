package car

import (
	"github.com/hyphacoop/go-dasl/cid"
)

const (
	MediaType     string = "application/vnd.ipld.car"
	FileExtension string = ".car"
	HeaderVersion uint64 = 1

	defaultMaxHeaderSize uint64 = 8 * 1024
	defaultMaxBlockSize  uint64 = 5 * 1024 * 1024
)

type Header struct {
	Version uint64 `cbor:"version" json:"version"`
	// TODO: cbor annotation to require this field when parsing
	Roots []cid.Cid `cbor:"roots" json:"roots"`
}
