package models

import (
	"bytes"
	"crypto/rand"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestChunkData_SmallEvent(t *testing.T) {
	// Small event should still be chunked (single chunk) with header
	data := []byte("small event data")

	chunks, err := chunkData(data)
	require.NoError(t, err)
	require.Len(t, chunks, 1)

	// Verify header
	header, err := decodeHeader(chunks[0])
	require.NoError(t, err)
	require.Equal(t, uint16(1), header.TotalChunks)
	require.Equal(t, uint32(len(data)), header.UncompressedSize)
	// Small data should not be compressed
	require.Equal(t, uint8(0), header.Flags&flagCompressed)

	// Verify round-trip
	reassembled, err := reassembleChunks(chunks)
	require.NoError(t, err)
	require.Equal(t, data, reassembled)
}

func TestChunkData_CompressionThreshold(t *testing.T) {
	// Data just under threshold should not be compressed
	smallData := make([]byte, CompressionThreshold-1)
	for i := range smallData {
		smallData[i] = byte(i % 256)
	}

	chunks, err := chunkData(smallData)
	require.NoError(t, err)

	header, err := decodeHeader(chunks[0])
	require.NoError(t, err)
	require.Equal(t, uint8(0), header.Flags&flagCompressed)

	// Data at threshold should be compressed (if it compresses well)
	largeData := make([]byte, CompressionThreshold+1)
	// Fill with compressible data (repeated pattern)
	for i := range largeData {
		largeData[i] = byte(i % 10)
	}

	chunks, err = chunkData(largeData)
	require.NoError(t, err)

	header, err = decodeHeader(chunks[0])
	require.NoError(t, err)
	require.Equal(t, flagCompressed, header.Flags&flagCompressed)
	require.Equal(t, uint32(len(largeData)), header.UncompressedSize)

	// Verify round-trip
	reassembled, err := reassembleChunks(chunks)
	require.NoError(t, err)
	require.Equal(t, largeData, reassembled)
}

func TestChunkData_LargeEvent_MultipleChunks(t *testing.T) {
	// Create data larger than chunk size
	dataSize := ChunkSize * 2.5
	data := make([]byte, int(dataSize))
	_, err := rand.Read(data)
	require.NoError(t, err)

	chunks, err := chunkData(data)
	require.NoError(t, err)
	require.Greater(t, len(chunks), 1)

	// Verify header
	header, err := decodeHeader(chunks[0])
	require.NoError(t, err)
	require.Equal(t, uint16(len(chunks)), header.TotalChunks)
	require.Equal(t, uint32(len(data)), header.UncompressedSize)

	// Random data doesn't compress well, so it might not be compressed
	// Just verify round-trip works
	reassembled, err := reassembleChunks(chunks)
	require.NoError(t, err)
	require.Equal(t, data, reassembled)
}

func TestChunkData_VeryLargeEvent(t *testing.T) {
	// 2MB event (ATProto max size) with random data that doesn't compress well
	dataSize := 2 * 1024 * 1024
	data := make([]byte, dataSize)
	_, err := rand.Read(data)
	require.NoError(t, err)

	chunks, err := chunkData(data)
	require.NoError(t, err)
	require.Greater(t, len(chunks), 1, "2MB of random data should require multiple chunks")

	header, err := decodeHeader(chunks[0])
	require.NoError(t, err)
	require.Equal(t, uint16(len(chunks)), header.TotalChunks)
	require.Equal(t, uint32(dataSize), header.UncompressedSize)

	// Verify round-trip
	reassembled, err := reassembleChunks(chunks)
	require.NoError(t, err)
	require.Equal(t, data, reassembled)
}

func TestChunkData_CompressionBenefit(t *testing.T) {
	// Data that compresses well should result in fewer chunks
	dataSize := ChunkSize * 3
	compressibleData := bytes.Repeat([]byte("hello world "), dataSize/12)

	chunks, err := chunkData(compressibleData)
	require.NoError(t, err)

	header, err := decodeHeader(chunks[0])
	require.NoError(t, err)
	require.Equal(t, flagCompressed, header.Flags&flagCompressed)

	// With good compression, we should have far fewer chunks than uncompressed would need
	totalChunkBytes := 0
	for _, chunk := range chunks {
		totalChunkBytes += len(chunk)
	}
	require.Less(t, totalChunkBytes, len(compressibleData)/2, "compression should reduce size significantly")

	// Verify round-trip
	reassembled, err := reassembleChunks(chunks)
	require.NoError(t, err)
	require.Equal(t, compressibleData, reassembled)
}

func TestChunkData_IncompressibleData(t *testing.T) {
	// Random data doesn't compress well
	dataSize := CompressionThreshold * 2
	randomData := make([]byte, dataSize)
	_, err := rand.Read(randomData)
	require.NoError(t, err)

	chunks, err := chunkData(randomData)
	require.NoError(t, err)

	// Random data might not be compressed if compression doesn't help
	// Either way, round-trip should work
	reassembled, err := reassembleChunks(chunks)
	require.NoError(t, err)
	require.Equal(t, randomData, reassembled)
}

func TestReassembleChunks_ChunkCountMismatch(t *testing.T) {
	data := []byte("test data")
	chunks, err := chunkData(data)
	require.NoError(t, err)

	// Remove a chunk
	if len(chunks) > 1 {
		chunks = chunks[:len(chunks)-1]
		_, err = reassembleChunks(chunks)
		require.Error(t, err)
		require.Contains(t, err.Error(), "chunk count mismatch")
	}
}

func TestReassembleChunks_EmptyChunks(t *testing.T) {
	_, err := reassembleChunks([][]byte{})
	require.Error(t, err)
	require.Contains(t, err.Error(), "no chunks")
}

func TestReassembleChunks_InvalidHeader(t *testing.T) {
	// Header too short
	_, err := reassembleChunks([][]byte{{1, 2, 3}})
	require.Error(t, err)
	require.Contains(t, err.Error(), "chunk header too short")
}

func TestChunkHeader_EncodeDecode(t *testing.T) {
	original := &chunkHeader{
		Flags:            flagCompressed,
		TotalChunks:      42,
		UncompressedSize: 123456,
	}

	encoded := original.encode()
	require.Len(t, encoded, chunkHeaderSize)

	decoded, err := decodeHeader(encoded)
	require.NoError(t, err)
	require.Equal(t, original.Flags, decoded.Flags)
	require.Equal(t, original.TotalChunks, decoded.TotalChunks)
	require.Equal(t, original.UncompressedSize, decoded.UncompressedSize)
}

func BenchmarkChunkData_Small(b *testing.B) {
	data := make([]byte, 1024) // 1KB
	for i := range data {
		data[i] = byte(i % 256)
	}

	b.ResetTimer()
	for b.Loop() {
		_, _ = chunkData(data)
	}
}

func BenchmarkChunkData_Medium(b *testing.B) {
	data := make([]byte, 50*1024) // 50KB
	for i := range data {
		data[i] = byte(i % 256)
	}

	b.ResetTimer()
	for b.Loop() {
		_, _ = chunkData(data)
	}
}

func BenchmarkChunkData_Large(b *testing.B) {
	data := make([]byte, 500*1024) // 500KB
	for i := range data {
		data[i] = byte(i % 256)
	}

	b.ResetTimer()
	for b.Loop() {
		_, _ = chunkData(data)
	}
}

func BenchmarkReassembleChunks(b *testing.B) {
	data := make([]byte, 500*1024) // 500KB
	for i := range data {
		data[i] = byte(i % 256)
	}
	chunks, _ := chunkData(data)

	b.ResetTimer()
	for b.Loop() {
		_, _ = reassembleChunks(chunks)
	}
}
