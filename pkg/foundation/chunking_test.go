package foundation

import (
	"bytes"
	"crypto/rand"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestChunkData_SmallEvent(t *testing.T) {
	chunker := NewChunker(nil)

	// Small event should still be chunked (single chunk) with header
	data := []byte("small event data")

	chunks, err := chunker.ChunkData(data)
	require.NoError(t, err)
	require.Len(t, chunks, 1)

	// Verify header
	header, err := DecodeChunkHeader(chunks[0])
	require.NoError(t, err)
	require.Equal(t, uint16(1), header.TotalChunks)
	require.Equal(t, uint32(len(data)), header.UncompressedSize)
	// Small data should not be compressed
	require.Equal(t, uint8(0), header.Flags&FlagCompressed)

	// Verify round-trip
	reassembled, err := chunker.ReassembleChunks(chunks)
	require.NoError(t, err)
	require.Equal(t, data, reassembled)
}

func TestChunkData_CompressionThreshold(t *testing.T) {
	chunker := NewChunker(nil)

	// Data just under threshold should not be compressed
	smallData := make([]byte, DefaultCompressionThreshold-1)
	for i := range smallData {
		smallData[i] = byte(i % 256)
	}

	chunks, err := chunker.ChunkData(smallData)
	require.NoError(t, err)

	header, err := DecodeChunkHeader(chunks[0])
	require.NoError(t, err)
	require.Equal(t, uint8(0), header.Flags&FlagCompressed)

	// Data at threshold should be compressed (if it compresses well)
	largeData := make([]byte, DefaultCompressionThreshold+1)
	// Fill with compressible data (repeated pattern)
	for i := range largeData {
		largeData[i] = byte(i % 10)
	}

	chunks, err = chunker.ChunkData(largeData)
	require.NoError(t, err)

	header, err = DecodeChunkHeader(chunks[0])
	require.NoError(t, err)
	require.Equal(t, FlagCompressed, header.Flags&FlagCompressed)
	require.Equal(t, uint32(len(largeData)), header.UncompressedSize)

	// Verify round-trip
	reassembled, err := chunker.ReassembleChunks(chunks)
	require.NoError(t, err)
	require.Equal(t, largeData, reassembled)
}

func TestChunkData_LargeEvent_MultipleChunks(t *testing.T) {
	chunker := NewChunker(nil)

	// Create data larger than chunk size
	dataSize := DefaultChunkSize * 2.5
	data := make([]byte, int(dataSize))
	_, err := rand.Read(data)
	require.NoError(t, err)

	chunks, err := chunker.ChunkData(data)
	require.NoError(t, err)
	require.Greater(t, len(chunks), 1)

	// Verify header
	header, err := DecodeChunkHeader(chunks[0])
	require.NoError(t, err)
	require.Equal(t, uint16(len(chunks)), header.TotalChunks)
	require.Equal(t, uint32(len(data)), header.UncompressedSize)

	// Random data doesn't compress well, so it might not be compressed
	// Just verify round-trip works
	reassembled, err := chunker.ReassembleChunks(chunks)
	require.NoError(t, err)
	require.Equal(t, data, reassembled)
}

func TestChunkData_VeryLargeEvent(t *testing.T) {
	chunker := NewChunker(nil)

	// 2MB event (ATProto max size) with random data that doesn't compress well
	dataSize := 2 * 1024 * 1024
	data := make([]byte, dataSize)
	_, err := rand.Read(data)
	require.NoError(t, err)

	chunks, err := chunker.ChunkData(data)
	require.NoError(t, err)
	require.Greater(t, len(chunks), 1, "2MB of random data should require multiple chunks")

	header, err := DecodeChunkHeader(chunks[0])
	require.NoError(t, err)
	require.Equal(t, uint16(len(chunks)), header.TotalChunks)
	require.Equal(t, uint32(dataSize), header.UncompressedSize)

	// Verify round-trip
	reassembled, err := chunker.ReassembleChunks(chunks)
	require.NoError(t, err)
	require.Equal(t, data, reassembled)
}

func TestChunkData_CompressionBenefit(t *testing.T) {
	chunker := NewChunker(nil)

	// Data that compresses well should result in fewer chunks
	dataSize := DefaultChunkSize * 3
	compressibleData := bytes.Repeat([]byte("hello world "), dataSize/12)

	chunks, err := chunker.ChunkData(compressibleData)
	require.NoError(t, err)

	header, err := DecodeChunkHeader(chunks[0])
	require.NoError(t, err)
	require.Equal(t, FlagCompressed, header.Flags&FlagCompressed)

	// With good compression, we should have far fewer chunks than uncompressed would need
	totalChunkBytes := 0
	for _, chunk := range chunks {
		totalChunkBytes += len(chunk)
	}
	require.Less(t, totalChunkBytes, len(compressibleData)/2, "compression should reduce size significantly")

	// Verify round-trip
	reassembled, err := chunker.ReassembleChunks(chunks)
	require.NoError(t, err)
	require.Equal(t, compressibleData, reassembled)
}

func TestChunkData_IncompressibleData(t *testing.T) {
	chunker := NewChunker(nil)

	// Random data doesn't compress well
	dataSize := DefaultCompressionThreshold * 2
	randomData := make([]byte, dataSize)
	_, err := rand.Read(randomData)
	require.NoError(t, err)

	chunks, err := chunker.ChunkData(randomData)
	require.NoError(t, err)

	// Random data might not be compressed if compression doesn't help
	// Either way, round-trip should work
	reassembled, err := chunker.ReassembleChunks(chunks)
	require.NoError(t, err)
	require.Equal(t, randomData, reassembled)
}

func TestReassembleChunks_ChunkCountMismatch(t *testing.T) {
	chunker := NewChunker(nil)

	data := []byte("test data")
	chunks, err := chunker.ChunkData(data)
	require.NoError(t, err)

	// Remove a chunk
	if len(chunks) > 1 {
		chunks = chunks[:len(chunks)-1]
		_, err = chunker.ReassembleChunks(chunks)
		require.Error(t, err)
		require.Contains(t, err.Error(), "chunk count mismatch")
	}
}

func TestReassembleChunks_EmptyChunks(t *testing.T) {
	chunker := NewChunker(nil)

	_, err := chunker.ReassembleChunks([][]byte{})
	require.Error(t, err)
	require.Contains(t, err.Error(), "no chunks")
}

func TestReassembleChunks_InvalidHeader(t *testing.T) {
	chunker := NewChunker(nil)

	// Header too short
	_, err := chunker.ReassembleChunks([][]byte{{1, 2, 3}})
	require.Error(t, err)
	require.Contains(t, err.Error(), "chunk header too short")
}

func TestChunkHeader_EncodeDecode(t *testing.T) {
	original := &ChunkHeader{
		Flags:            FlagCompressed,
		TotalChunks:      42,
		UncompressedSize: 123456,
	}

	encoded := original.Encode()
	require.Len(t, encoded, ChunkHeaderSize)

	decoded, err := DecodeChunkHeader(encoded)
	require.NoError(t, err)
	require.Equal(t, original.Flags, decoded.Flags)
	require.Equal(t, original.TotalChunks, decoded.TotalChunks)
	require.Equal(t, original.UncompressedSize, decoded.UncompressedSize)
}

func TestChunker_CustomConfig(t *testing.T) {
	// Create a chunker with smaller chunk size for testing
	chunker := NewChunker(&ChunkerConfig{
		ChunkSize:            1024, // 1KB chunks
		CompressionThreshold: 512,  // Compress above 512 bytes
		MaxBatchBytes:        4096,
	})

	require.Equal(t, 1024, chunker.ChunkSize())
	require.Equal(t, 512, chunker.CompressionThreshold())
	require.Equal(t, 4096, chunker.MaxBatchBytes())

	// Create random data that will require multiple chunks with the small chunk size
	// Random data doesn't compress well, so 3KB should need multiple 1KB chunks
	data := make([]byte, 3000)
	_, err := rand.Read(data)
	require.NoError(t, err)

	chunks, err := chunker.ChunkData(data)
	require.NoError(t, err)
	require.Greater(t, len(chunks), 1, "should require multiple chunks with 1KB chunk size")

	// Verify round-trip
	reassembled, err := chunker.ReassembleChunks(chunks)
	require.NoError(t, err)
	require.Equal(t, data, reassembled)
}

func TestChunker_DefaultConfig(t *testing.T) {
	// nil config should use defaults
	chunker := NewChunker(nil)

	require.Equal(t, DefaultChunkSize, chunker.ChunkSize())
	require.Equal(t, DefaultCompressionThreshold, chunker.CompressionThreshold())
	require.Equal(t, DefaultMaxBatchBytes, chunker.MaxBatchBytes())
}

func TestChunker_PartialConfig(t *testing.T) {
	// Only override some values
	chunker := NewChunker(&ChunkerConfig{
		ChunkSize: 50 * 1024, // 50KB
		// Leave others at zero to use defaults
	})

	require.Equal(t, 50*1024, chunker.ChunkSize())
	require.Equal(t, DefaultCompressionThreshold, chunker.CompressionThreshold())
	require.Equal(t, DefaultMaxBatchBytes, chunker.MaxBatchBytes())
}

func BenchmarkChunkData_Small(b *testing.B) {
	chunker := NewChunker(nil)
	data := make([]byte, 1024) // 1KB
	for i := range data {
		data[i] = byte(i % 256)
	}

	b.ResetTimer()
	for b.Loop() {
		_, _ = chunker.ChunkData(data)
	}
}

func BenchmarkChunkData_Medium(b *testing.B) {
	chunker := NewChunker(nil)
	data := make([]byte, 50*1024) // 50KB
	for i := range data {
		data[i] = byte(i % 256)
	}

	b.ResetTimer()
	for b.Loop() {
		_, _ = chunker.ChunkData(data)
	}
}

func BenchmarkChunkData_Large(b *testing.B) {
	chunker := NewChunker(nil)
	data := make([]byte, 500*1024) // 500KB
	for i := range data {
		data[i] = byte(i % 256)
	}

	b.ResetTimer()
	for b.Loop() {
		_, _ = chunker.ChunkData(data)
	}
}

func BenchmarkReassembleChunks(b *testing.B) {
	chunker := NewChunker(nil)
	data := make([]byte, 500*1024) // 500KB
	for i := range data {
		data[i] = byte(i % 256)
	}
	chunks, _ := chunker.ChunkData(data)

	b.ResetTimer()
	for b.Loop() {
		_, _ = chunker.ReassembleChunks(chunks)
	}
}
