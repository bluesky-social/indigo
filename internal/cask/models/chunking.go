package models

import (
	"encoding/binary"
	"fmt"

	"github.com/klauspost/compress/zstd"
)

const (
	// ChunkSize is the maximum size of each chunk stored in FDB (under 100KB limit)
	ChunkSize = 90 * 1024

	// CompressionThreshold is the minimum event size before compression is applied
	CompressionThreshold = 5 * 1024

	// MaxBatchBytes is the maximum bytes to read in a single GetEventsSince call
	// to stay under FDB's 10MB transaction limit
	MaxBatchBytes = 8 * 1024 * 1024

	// chunkHeaderSize is the size of the header in the first chunk:
	// flags (1 byte) + total_chunks (2 bytes) + uncompressed_size (4 bytes)
	chunkHeaderSize = 7

	// flagCompressed indicates the data is zstd compressed
	flagCompressed uint8 = 1 << 0
)

var (
	// zstd encoder/decoder are safe for concurrent use
	zstdEncoder, _ = zstd.NewWriter(nil, zstd.WithEncoderLevel(zstd.SpeedDefault))
	zstdDecoder, _ = zstd.NewReader(nil)
)

// chunkHeader represents the metadata stored at the start of the first chunk
type chunkHeader struct {
	Flags            uint8
	TotalChunks      uint16
	UncompressedSize uint32
}

// encodeHeader writes the chunk header to a byte slice
func (h *chunkHeader) encode() []byte {
	buf := make([]byte, chunkHeaderSize)
	buf[0] = h.Flags
	binary.BigEndian.PutUint16(buf[1:3], h.TotalChunks)
	binary.BigEndian.PutUint32(buf[3:7], h.UncompressedSize)
	return buf
}

// decodeHeader reads a chunk header from a byte slice
func decodeHeader(data []byte) (*chunkHeader, error) {
	if len(data) < chunkHeaderSize {
		return nil, fmt.Errorf("chunk header too short: %d bytes", len(data))
	}
	return &chunkHeader{
		Flags:            data[0],
		TotalChunks:      binary.BigEndian.Uint16(data[1:3]),
		UncompressedSize: binary.BigEndian.Uint32(data[3:7]),
	}, nil
}

// chunkData splits data into chunks for storage. If the data exceeds CompressionThreshold,
// it will be compressed first. Returns the chunks (first chunk includes header) and any error.
func chunkData(data []byte) ([][]byte, error) {
	originalSize := len(data)
	compressed := false

	// Compress if over threshold
	if len(data) >= CompressionThreshold {
		compressedData := zstdEncoder.EncodeAll(data, nil)
		// Only use compressed version if it's actually smaller
		if len(compressedData) < len(data) {
			data = compressedData
			compressed = true
		}
	}

	// Calculate how many chunks we need
	// First chunk has header overhead, so it holds less data
	firstChunkDataSize := ChunkSize - chunkHeaderSize
	if len(data) <= firstChunkDataSize {
		// Single chunk
		header := &chunkHeader{
			TotalChunks:      1,
			UncompressedSize: uint32(originalSize),
		}
		if compressed {
			header.Flags |= flagCompressed
		}

		chunk := make([]byte, 0, chunkHeaderSize+len(data))
		chunk = append(chunk, header.encode()...)
		chunk = append(chunk, data...)
		return [][]byte{chunk}, nil
	}

	// Multiple chunks needed
	remainingAfterFirst := len(data) - firstChunkDataSize
	additionalChunks := (remainingAfterFirst + ChunkSize - 1) / ChunkSize
	totalChunks := 1 + additionalChunks

	if totalChunks > 65535 {
		return nil, fmt.Errorf("data too large: would require %d chunks (max 65535)", totalChunks)
	}

	header := &chunkHeader{
		TotalChunks:      uint16(totalChunks),
		UncompressedSize: uint32(originalSize),
	}
	if compressed {
		header.Flags |= flagCompressed
	}

	chunks := make([][]byte, 0, totalChunks)

	// First chunk with header
	firstChunk := make([]byte, 0, ChunkSize)
	firstChunk = append(firstChunk, header.encode()...)
	firstChunk = append(firstChunk, data[:firstChunkDataSize]...)
	chunks = append(chunks, firstChunk)

	// Remaining chunks
	offset := firstChunkDataSize
	for offset < len(data) {
		end := min(offset+ChunkSize, len(data))
		chunks = append(chunks, data[offset:end])
		offset = end
	}

	return chunks, nil
}

// reassembleChunks reconstructs the original data from chunks.
// The first chunk must contain the header, subsequent chunks are raw data.
func reassembleChunks(chunks [][]byte) ([]byte, error) {
	if len(chunks) == 0 {
		return nil, fmt.Errorf("no chunks provided")
	}

	// Parse header from first chunk
	header, err := decodeHeader(chunks[0])
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
	data = append(data, chunks[0][chunkHeaderSize:]...)

	// Remaining chunks
	for i := 1; i < len(chunks); i++ {
		data = append(data, chunks[i]...)
	}

	// Decompress if needed
	if header.Flags&flagCompressed != 0 {
		decompressed, err := zstdDecoder.DecodeAll(data, nil)
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
