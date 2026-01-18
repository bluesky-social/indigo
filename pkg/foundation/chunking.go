package foundation

import (
	"encoding/binary"
	"fmt"

	"github.com/klauspost/compress/zstd"
)

const (
	// DefaultChunkSize is the default maximum size of each chunk stored in FDB (under 100KB limit)
	DefaultChunkSize = 90 * 1024

	// DefaultCompressionThreshold is the default minimum event size before compression is applied
	DefaultCompressionThreshold = 5 * 1024

	// DefaultMaxBatchBytes is the default maximum bytes to read in a single batch
	// to stay under FDB's 10MB transaction limit
	DefaultMaxBatchBytes = 8 * 1024 * 1024

	// ChunkHeaderSize is the size of the header in the first chunk:
	// flags (1 byte) + total_chunks (2 bytes) + uncompressed_size (4 bytes)
	ChunkHeaderSize = 7

	// FlagCompressed indicates the data is zstd compressed
	FlagCompressed uint8 = 1 << 0
)

// ChunkerConfig holds configuration for chunking behavior
type ChunkerConfig struct {
	// ChunkSize is the maximum size of each chunk (default: 90KB)
	ChunkSize int

	// CompressionThreshold is the minimum data size before compression is applied (default: 5KB)
	CompressionThreshold int

	// MaxBatchBytes is the maximum bytes to read in a single batch (default: 8MB)
	MaxBatchBytes int
}

// Chunker handles splitting large data into chunks with optional compression
type Chunker struct {
	chunkSize            int
	compressionThreshold int
	maxBatchBytes        int

	encoder *zstd.Encoder
	decoder *zstd.Decoder
}

// NewChunker creates a new Chunker with the given configuration.
// If cfg is nil, default values are used.
func NewChunker(cfg *ChunkerConfig) *Chunker {
	c := &Chunker{
		chunkSize:            DefaultChunkSize,
		compressionThreshold: DefaultCompressionThreshold,
		maxBatchBytes:        DefaultMaxBatchBytes,
	}

	if cfg != nil {
		if cfg.ChunkSize > 0 {
			c.chunkSize = cfg.ChunkSize
		}
		if cfg.CompressionThreshold > 0 {
			c.compressionThreshold = cfg.CompressionThreshold
		}
		if cfg.MaxBatchBytes > 0 {
			c.maxBatchBytes = cfg.MaxBatchBytes
		}
	}

	// zstd encoder/decoder are safe for concurrent use
	c.encoder, _ = zstd.NewWriter(nil, zstd.WithEncoderLevel(zstd.SpeedDefault))
	c.decoder, _ = zstd.NewReader(nil)

	return c
}

// ChunkSize returns the configured chunk size
func (c *Chunker) ChunkSize() int {
	return c.chunkSize
}

// CompressionThreshold returns the configured compression threshold
func (c *Chunker) CompressionThreshold() int {
	return c.compressionThreshold
}

// MaxBatchBytes returns the configured max batch bytes
func (c *Chunker) MaxBatchBytes() int {
	return c.maxBatchBytes
}

// ChunkHeader represents the metadata stored at the start of the first chunk
type ChunkHeader struct {
	Flags            uint8
	TotalChunks      uint16
	UncompressedSize uint32
}

// Encode writes the chunk header to a byte slice
func (h *ChunkHeader) Encode() []byte {
	buf := make([]byte, ChunkHeaderSize)
	buf[0] = h.Flags
	binary.BigEndian.PutUint16(buf[1:3], h.TotalChunks)
	binary.BigEndian.PutUint32(buf[3:7], h.UncompressedSize)
	return buf
}

// DecodeChunkHeader reads a chunk header from a byte slice
func DecodeChunkHeader(data []byte) (*ChunkHeader, error) {
	if len(data) < ChunkHeaderSize {
		return nil, fmt.Errorf("chunk header too short: %d bytes", len(data))
	}
	return &ChunkHeader{
		Flags:            data[0],
		TotalChunks:      binary.BigEndian.Uint16(data[1:3]),
		UncompressedSize: binary.BigEndian.Uint32(data[3:7]),
	}, nil
}

// ChunkData splits data into chunks for storage. If the data exceeds the compression
// threshold, it will be compressed first. Returns the chunks (first chunk includes header).
func (c *Chunker) ChunkData(data []byte) ([][]byte, error) {
	originalSize := len(data)
	compressed := false

	// Compress if over threshold
	if len(data) >= c.compressionThreshold {
		compressedData := c.encoder.EncodeAll(data, nil)
		// Only use compressed version if it's actually smaller
		if len(compressedData) < len(data) {
			data = compressedData
			compressed = true
		}
	}

	// Calculate how many chunks we need
	// First chunk has header overhead, so it holds less data
	firstChunkDataSize := c.chunkSize - ChunkHeaderSize
	if len(data) <= firstChunkDataSize {
		// Single chunk
		header := &ChunkHeader{
			TotalChunks:      1,
			UncompressedSize: uint32(originalSize),
		}
		if compressed {
			header.Flags |= FlagCompressed
		}

		chunk := make([]byte, 0, ChunkHeaderSize+len(data))
		chunk = append(chunk, header.Encode()...)
		chunk = append(chunk, data...)
		return [][]byte{chunk}, nil
	}

	// Multiple chunks needed
	remainingAfterFirst := len(data) - firstChunkDataSize
	additionalChunks := (remainingAfterFirst + c.chunkSize - 1) / c.chunkSize
	totalChunks := 1 + additionalChunks

	if totalChunks > 65535 {
		return nil, fmt.Errorf("data too large: would require %d chunks (max 65535)", totalChunks)
	}

	header := &ChunkHeader{
		TotalChunks:      uint16(totalChunks),
		UncompressedSize: uint32(originalSize),
	}
	if compressed {
		header.Flags |= FlagCompressed
	}

	chunks := make([][]byte, 0, totalChunks)

	// First chunk with header
	firstChunk := make([]byte, 0, c.chunkSize)
	firstChunk = append(firstChunk, header.Encode()...)
	firstChunk = append(firstChunk, data[:firstChunkDataSize]...)
	chunks = append(chunks, firstChunk)

	// Remaining chunks
	offset := firstChunkDataSize
	for offset < len(data) {
		end := min(offset+c.chunkSize, len(data))
		chunks = append(chunks, data[offset:end])
		offset = end
	}

	return chunks, nil
}

// ReassembleChunks reconstructs the original data from chunks.
// The first chunk must contain the header, subsequent chunks are raw data.
func (c *Chunker) ReassembleChunks(chunks [][]byte) ([]byte, error) {
	if len(chunks) == 0 {
		return nil, fmt.Errorf("no chunks provided")
	}

	// Parse header from first chunk
	header, err := DecodeChunkHeader(chunks[0])
	if err != nil {
		return nil, fmt.Errorf("failed to decode chunk header: %w", err)
	}

	if int(header.TotalChunks) != len(chunks) {
		return nil, fmt.Errorf("chunk count mismatch: header says %d, got %d", header.TotalChunks, len(chunks))
	}

	// Reassemble data
	// Estimate total size (compressed size unknown, but we can grow as needed)
	var data []byte

	// First chunk data (after header)
	data = append(data, chunks[0][ChunkHeaderSize:]...)

	// Remaining chunks
	for i := 1; i < len(chunks); i++ {
		data = append(data, chunks[i]...)
	}

	// Decompress if needed
	if header.Flags&FlagCompressed != 0 {
		decompressed, err := c.decoder.DecodeAll(data, nil)
		if err != nil {
			return nil, fmt.Errorf("failed to decompress data: %w", err)
		}
		data = decompressed
	}

	// Verify size matches
	if uint32(len(data)) != header.UncompressedSize {
		return nil, fmt.Errorf("size mismatch: expected %d, got %d", header.UncompressedSize, len(data))
	}

	return data, nil
}

// DefaultChunker is the package-level chunker with default configuration
var DefaultChunker = NewChunker(nil)

// ChunkData splits data into chunks using the default chunker.
// This is a convenience function; for custom configuration, create a Chunker instance.
func ChunkData(data []byte) ([][]byte, error) {
	return DefaultChunker.ChunkData(data)
}

// ReassembleChunks reconstructs data from chunks using the default chunker.
// This is a convenience function; for custom configuration, create a Chunker instance.
func ReassembleChunks(chunks [][]byte) ([]byte, error) {
	return DefaultChunker.ReassembleChunks(chunks)
}
