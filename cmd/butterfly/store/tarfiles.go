// Package store provides a tar file implementation of the Store interface
package store

import (
	"archive/tar"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/bluesky-social/indigo/cmd/butterfly/remote"
)

// TarfilesStore implements Store by writing repository data to tar files
type TarfilesStore struct {
	// The directory to store the .tar files
	// Each repository is stored as a single .tar file
	// The contents of the .tar file is a collection of json files
	// The directory structure is based on the collections
	Dirpath string

	// Internal state
	mu      sync.Mutex
	writers map[string]*tarWriter
	tempDir string
}

// tarWriter manages writing to a single tar file
type tarWriter struct {
	file      *os.File
	writer    *tar.Writer
	entries   map[string]bool // Track existing entries
	tempFile  string
	finalFile string
}

// NewTarfilesStore creates a new TarfilesStore
func NewTarfilesStore(dirpath string) *TarfilesStore {
	return &TarfilesStore{
		Dirpath: dirpath,
		writers: make(map[string]*tarWriter),
	}
}

// Setup creates the directory if it doesn't exist
func (t *TarfilesStore) Setup(ctx context.Context) error {
	if err := os.MkdirAll(t.Dirpath, 0755); err != nil {
		return fmt.Errorf("failed to create directory %s: %w", t.Dirpath, err)
	}

	// Create temp directory for atomic writes
	tempDir := filepath.Join(t.Dirpath, ".tmp")
	if err := os.MkdirAll(tempDir, 0755); err != nil {
		return fmt.Errorf("failed to create temp directory: %w", err)
	}
	t.tempDir = tempDir

	return nil
}

// Close finalizes all tar files and cleans up
func (t *TarfilesStore) Close() error {
	t.mu.Lock()
	defer t.mu.Unlock()

	var errs []error
	for did, tw := range t.writers {
		if err := t.finalizeTarWriter(tw); err != nil {
			errs = append(errs, fmt.Errorf("failed to finalize tar for %s: %w", did, err))
		}
	}

	// Clean up temp directory
	if t.tempDir != "" {
		os.RemoveAll(t.tempDir)
	}

	if len(errs) > 0 {
		return fmt.Errorf("errors closing tar files: %v", errs)
	}
	return nil
}

// Receive processes events from the stream
func (t *TarfilesStore) Receive(ctx context.Context, stream *remote.RemoteStream) error {
	for event := range stream.Ch {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		if event.Kind != remote.EventKindCommit || event.Commit == nil {
			continue
		}

		if err := t.processCommit(ctx, event.Did, event.Commit); err != nil {
			// Log error but continue processing
			fmt.Fprintf(os.Stderr, "tarfiles: error processing commit for %s: %v\n", event.Did, err)
		}
	}
	return nil
}

// processCommit handles a single commit event
func (t *TarfilesStore) processCommit(ctx context.Context, did string, commit *remote.StreamEventCommit) error {
	t.mu.Lock()
	tw, err := t.getTarWriter(did)
	t.mu.Unlock()
	if err != nil {
		return fmt.Errorf("failed to get tar writer: %w", err)
	}

	entryPath := fmt.Sprintf("%s/%s.json", commit.Collection, commit.Rkey)

	switch commit.Operation {
	case remote.OpDelete:
		// Write a JSON object with _deleted: true
		deletedRecord := map[string]any{"_deleted": true}
		data, err := json.MarshalIndent(deletedRecord, "", "  ")
		if err != nil {
			return fmt.Errorf("failed to marshal deleted record: %w", err)
		}
		return t.writeTarEntry(tw, entryPath, data)

	case remote.OpCreate, remote.OpUpdate:
		// Marshal record to JSON
		data, err := json.MarshalIndent(commit.Record, "", "  ")
		if err != nil {
			return fmt.Errorf("failed to marshal record: %w", err)
		}
		return t.writeTarEntry(tw, entryPath, data)

	default:
		return fmt.Errorf("unknown operation: %s", commit.Operation)
	}
}

// getTarWriter gets or creates a tar writer for a DID
func (t *TarfilesStore) getTarWriter(did string) (*tarWriter, error) {
	if tw, exists := t.writers[did]; exists {
		return tw, nil
	}

	// Sanitize DID for filename
	filename := strings.ReplaceAll(did, ":", "_")
	finalPath := filepath.Join(t.Dirpath, filename+".tar")
	tempPath := filepath.Join(t.tempDir, filename+".tar.tmp")

	// Check if tar file already exists and load entries
	entries := make(map[string]bool)
	if _, err := os.Stat(finalPath); err == nil {
		if err := t.loadExistingEntries(finalPath, entries); err != nil {
			return nil, fmt.Errorf("failed to load existing tar: %w", err)
		}
	}

	// Create new temp file
	file, err := os.Create(tempPath)
	if err != nil {
		return nil, fmt.Errorf("failed to create tar file: %w", err)
	}

	// Create tar writer first
	newTarWriter := tar.NewWriter(file)

	// If we had existing entries, copy them to the new tar
	if len(entries) > 0 {
		if err := t.copyExistingTarEntries(finalPath, newTarWriter, entries); err != nil {
			newTarWriter.Close()
			file.Close()
			os.Remove(tempPath)
			return nil, fmt.Errorf("failed to copy existing tar: %w", err)
		}
	}

	tw := &tarWriter{
		file:      file,
		writer:    newTarWriter,
		entries:   entries,
		tempFile:  tempPath,
		finalFile: finalPath,
	}

	t.writers[did] = tw
	return tw, nil
}

// loadExistingEntries reads the list of entries from an existing tar file
func (t *TarfilesStore) loadExistingEntries(path string, entries map[string]bool) error {
	file, err := os.Open(path)
	if err != nil {
		return err
	}
	defer file.Close()

	reader := tar.NewReader(file)
	for {
		header, err := reader.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return err
		}
		entries[header.Name] = true
	}
	return nil
}

// copyExistingTarEntries copies entries from an existing tar file to a tar writer
func (t *TarfilesStore) copyExistingTarEntries(srcPath string, writer *tar.Writer, entries map[string]bool) error {
	src, err := os.Open(srcPath)
	if err != nil {
		return err
	}
	defer src.Close()

	reader := tar.NewReader(src)

	for {
		header, err := reader.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return err
		}

		// Copy the entry
		if err := writer.WriteHeader(header); err != nil {
			return err
		}
		if _, err := io.Copy(writer, reader); err != nil {
			return err
		}
	}

	return nil
}

// writeTarEntry writes a single entry to the tar file
func (t *TarfilesStore) writeTarEntry(tw *tarWriter, path string, data []byte) error {
	header := &tar.Header{
		Name:    path,
		Mode:    0644,
		Size:    int64(len(data)),
		ModTime: time.Now(),
	}

	if err := tw.writer.WriteHeader(header); err != nil {
		return fmt.Errorf("failed to write tar header: %w", err)
	}

	if _, err := tw.writer.Write(data); err != nil {
		return fmt.Errorf("failed to write tar data: %w", err)
	}

	tw.entries[path] = true
	return nil
}

// finalizeTarWriter closes a tar writer and moves the temp file to final location
func (t *TarfilesStore) finalizeTarWriter(tw *tarWriter) error {
	if err := tw.writer.Close(); err != nil {
		return fmt.Errorf("failed to close tar writer: %w", err)
	}
	if err := tw.file.Close(); err != nil {
		return fmt.Errorf("failed to close file: %w", err)
	}

	// Atomically move temp file to final location
	if err := os.Rename(tw.tempFile, tw.finalFile); err != nil {
		return fmt.Errorf("failed to move tar file: %w", err)
	}

	return nil
}

// ReadTarFile reads a tar file and returns its contents (for debugging/testing)
func ReadTarFile(path string) (map[string][]byte, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()

	contents := make(map[string][]byte)
	reader := tar.NewReader(file)

	for {
		header, err := reader.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, err
		}

		buf := new(bytes.Buffer)
		if _, err := io.Copy(buf, reader); err != nil {
			return nil, err
		}
		contents[header.Name] = buf.Bytes()
	}

	return contents, nil
}
