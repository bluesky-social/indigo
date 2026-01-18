package foundation

import (
	"encoding/binary"
	"fmt"
	"sync"

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

	// Buffer pools to reduce allocations
	compressPool   sync.Pool // for compression output buffers
	reassemblePool sync.Pool // for reassembly buffers
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
	c.encoder, _ = zstd.NewWriter(nil,
		zstd.WithEncoderLevel(zstd.SpeedDefault),
		zstd.WithEncoderConcurrency(1), // Single-threaded per call, but encoder itself is reusable
	)
	c.decoder, _ = zstd.NewReader(nil,
		zstd.WithDecoderConcurrency(1),
	)

	// Initialize pools with reasonable starting sizes
	c.compressPool = sync.Pool{
		New: func() any {
			// Start with compression threshold size - most common case
			buf := make([]byte, 0, c.compressionThreshold)
			return &buf
		},
	}
	c.reassemblePool = sync.Pool{
		New: func() any {
			// Start with chunk size - typical reassembly size
			buf := make([]byte, 0, c.chunkSize)
			return &buf
		},
	}

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

// EncodeInto writes the chunk header into an existing byte slice.
// The slice must have at least ChunkHeaderSize bytes.
func (h *ChunkHeader) EncodeInto(buf []byte) {
	buf[0] = h.Flags
	binary.BigEndian.PutUint16(buf[1:3], h.TotalChunks)
	binary.BigEndian.PutUint32(buf[3:7], h.UncompressedSize)
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

// getCompressBuffer gets a buffer from the pool, ensuring it has sufficient capacity
func (c *Chunker) getCompressBuffer(size int) []byte {
	bufPtr, ok := c.compressPool.Get().(*[]byte)
	if !ok || bufPtr == nil {
		return make([]byte, 0, size)
	}
	buf := *bufPtr
	if cap(buf) < size {
		// Need a larger buffer
		return make([]byte, 0, size)
	}
	return buf[:0]
}

// putCompressBuffer returns a buffer to the pool
func (c *Chunker) putCompressBuffer(buf []byte) {
	// Only pool reasonably-sized buffers to avoid memory bloat
	if cap(buf) <= c.maxBatchBytes {
		c.compressPool.Put(&buf)
	}
}

// getReassembleBuffer gets a buffer from the pool, ensuring it has sufficient capacity
func (c *Chunker) getReassembleBuffer(size int) []byte {
	bufPtr, ok := c.reassemblePool.Get().(*[]byte)
	if !ok || bufPtr == nil {
		return make([]byte, 0, size)
	}
	buf := *bufPtr
	if cap(buf) < size {
		return make([]byte, 0, size)
	}
	return buf[:0]
}

// putReassembleBuffer returns a buffer to the pool
func (c *Chunker) putReassembleBuffer(buf []byte) {
	if cap(buf) <= c.maxBatchBytes {
		c.reassemblePool.Put(&buf)
	}
}

// ChunkData splits data into chunks for storage. If the data exceeds the compression
// threshold, it will be compressed first. Returns the chunks (first chunk includes header).
func (c *Chunker) ChunkData(data []byte) ([][]byte, error) {
	originalSize := len(data)
	compressed := false
	var compressBuf []byte

	// Compress if over threshold
	if len(data) >= c.compressionThreshold {
		// Get a buffer from pool for compression output
		// Estimate: compressed size is usually smaller than original
		compressBuf = c.getCompressBuffer(len(data))
		compressedData := c.encoder.EncodeAll(data, compressBuf)

		// Only use compressed version if it's actually smaller
		if len(compressedData) < len(data) {
			data = compressedData
			compressed = true
		} else {
			// Compression didn't help, return buffer to pool
			c.putCompressBuffer(compressBuf)
			compressBuf = nil
		}
	}

	// Calculate how many chunks we need
	// First chunk has header overhead, so it holds less data
	firstChunkDataSize := c.chunkSize - ChunkHeaderSize
	if len(data) <= firstChunkDataSize {
		// Single chunk - most common case, optimize for it
		chunk := make([]byte, ChunkHeaderSize+len(data))

		header := ChunkHeader{
			TotalChunks:      1,
			UncompressedSize: uint32(originalSize),
		}
		if compressed {
			header.Flags |= FlagCompressed
		}
		header.EncodeInto(chunk)
		copy(chunk[ChunkHeaderSize:], data)

		// Return compression buffer to pool if we used one
		if compressBuf != nil {
			c.putCompressBuffer(compressBuf)
		}

		return [][]byte{chunk}, nil
	}

	// Multiple chunks needed
	remainingAfterFirst := len(data) - firstChunkDataSize
	additionalChunks := (remainingAfterFirst + c.chunkSize - 1) / c.chunkSize
	totalChunks := 1 + additionalChunks

	if totalChunks > 65535 {
		if compressBuf != nil {
			c.putCompressBuffer(compressBuf)
		}
		return nil, fmt.Errorf("data too large: would require %d chunks (max 65535)", totalChunks)
	}

	header := ChunkHeader{
		TotalChunks:      uint16(totalChunks),
		UncompressedSize: uint32(originalSize),
	}
	if compressed {
		header.Flags |= FlagCompressed
	}

	chunks := make([][]byte, totalChunks)

	// First chunk with header
	firstChunk := make([]byte, ChunkHeaderSize+firstChunkDataSize)
	header.EncodeInto(firstChunk)
	copy(firstChunk[ChunkHeaderSize:], data[:firstChunkDataSize])
	chunks[0] = firstChunk

	// Remaining chunks - copy data to ensure independence from source buffer
	offset := firstChunkDataSize
	for i := 1; i < totalChunks; i++ {
		end := min(offset+c.chunkSize, len(data))
		chunkData := make([]byte, end-offset)
		copy(chunkData, data[offset:end])
		chunks[i] = chunkData
		offset = end
	}

	// Return compression buffer to pool
	if compressBuf != nil {
		c.putCompressBuffer(compressBuf)
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

	// Calculate total compressed size for pre-allocation
	compressedSize := 0
	for _, chunk := range chunks {
		compressedSize += len(chunk)
	}
	compressedSize -= ChunkHeaderSize // First chunk has header

	// Pre-allocate reassembly buffer
	data := c.getReassembleBuffer(compressedSize)

	// First chunk data (after header)
	data = append(data, chunks[0][ChunkHeaderSize:]...)

	// Remaining chunks
	for i := 1; i < len(chunks); i++ {
		data = append(data, chunks[i]...)
	}

	// Decompress if needed
	if header.Flags&FlagCompressed != 0 {
		// Pre-allocate decompression buffer with known uncompressed size
		decompressBuf := make([]byte, 0, header.UncompressedSize)
		decompressed, err := c.decoder.DecodeAll(data, decompressBuf)
		if err != nil {
			c.putReassembleBuffer(data)
			return nil, fmt.Errorf("failed to decompress data: %w", err)
		}

		// Return the compressed data buffer to pool
		c.putReassembleBuffer(data)
		data = decompressed
	}

	// Verify size matches
	if uint32(len(data)) != header.UncompressedSize {
		return nil, fmt.Errorf("size mismatch: expected %d, got %d", header.UncompressedSize, len(data))
	}

	return data, nil
}
