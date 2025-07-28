// Package store provides a tar file implementation of the Store interface
package store

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
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

// NOTE: do not work on this this until the Store interface is fully mature

// TarfilesStore implements Store by writing repository data to gzipped tar files
type TarfilesStore struct {
	// The directory to store the .tar.gz files
	// Each repository is stored as a single .tar.gz file
	// The contents of the .tar.gz file is a collection of json files
	// The directory structure is based on the collections
	Dirpath string

	// Internal state
	mu      sync.Mutex
	writers map[string]*tarWriter
	tempDir string
}

// tarWriter manages writing to a single tar file
type tarWriter struct {
	file       *os.File
	gzipWriter *gzip.Writer
	writer     *tar.Writer
	entries    map[string]bool // Track existing entries
	tempFile   string
	finalFile  string
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

// BackfillRepo resets a repo and re-ingests it from a remote stream
func (t *TarfilesStore) BackfillRepo(ctx context.Context, did string, stream *remote.RemoteStream) error {
	// TODO For now, it's fine to just reuse ActiveSync. A more optimized variant could be useful.
	return t.ActiveSync(ctx, stream)
}

// ActiveSync processes live update events from a remote stream
func (t *TarfilesStore) ActiveSync(ctx context.Context, stream *remote.RemoteStream) error {
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
	finalPath := filepath.Join(t.Dirpath, filename+".tar.gz")
	tempPath := filepath.Join(t.tempDir, filename+".tar.gz.tmp")

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

	// Create gzip writer first
	gzipWriter := gzip.NewWriter(file)
	// Create tar writer on top of gzip writer
	newTarWriter := tar.NewWriter(gzipWriter)

	// If we had existing entries, copy them to the new tar
	if len(entries) > 0 {
		if err := t.copyExistingTarEntries(finalPath, newTarWriter, entries); err != nil {
			newTarWriter.Close()
			gzipWriter.Close()
			file.Close()
			os.Remove(tempPath)
			return nil, fmt.Errorf("failed to copy existing tar: %w", err)
		}
	}

	tw := &tarWriter{
		file:       file,
		gzipWriter: gzipWriter,
		writer:     newTarWriter,
		entries:    entries,
		tempFile:   tempPath,
		finalFile:  finalPath,
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

	gzipReader, err := gzip.NewReader(file)
	if err != nil {
		return err
	}
	defer gzipReader.Close()

	reader := tar.NewReader(gzipReader)
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

	gzipReader, err := gzip.NewReader(src)
	if err != nil {
		return err
	}
	defer gzipReader.Close()

	reader := tar.NewReader(gzipReader)

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
	if err := tw.gzipWriter.Close(); err != nil {
		return fmt.Errorf("failed to close gzip writer: %w", err)
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

// ReadTarFile reads a gzipped tar file and returns its contents (for debugging/testing)
func ReadTarFile(path string) (map[string][]byte, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()

	gzipReader, err := gzip.NewReader(file)
	if err != nil {
		return nil, err
	}
	defer gzipReader.Close()

	contents := make(map[string][]byte)
	reader := tar.NewReader(gzipReader)

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

// KvGet retrieves a value from general KV storage
func (t *TarfilesStore) KvGet(namespace string, key string) (string, error) {
	// Sanitize namespace and key to prevent directory traversal
	namespace, key, err := sanitizeNamespaceAndKey(namespace, key)
	if err != nil {
		return "", err
	}

	// Build the file path
	filePath := filepath.Join(t.Dirpath, namespace, key+".json")

	// Read the file
	value, err := os.ReadFile(filePath)
	if err != nil {
		if os.IsNotExist(err) {
			return "", fmt.Errorf("key %q not found in namespace %q", key, namespace)
		}
		return "", fmt.Errorf("failed to read key file: %w", err)
	}

	return string(value), nil
}

// KvPut stores a value in general KV storage using atomic file operations
func (t *TarfilesStore) KvPut(namespace string, key string, value string) error {
	// Sanitize namespace and key to prevent directory traversal
	namespace, key, err := sanitizeNamespaceAndKey(namespace, key)
	if err != nil {
		return err
	}

	t.mu.Lock()
	defer t.mu.Unlock()

	// Create the namespace directory if it doesn't exist
	namespaceDir := filepath.Join(t.Dirpath, namespace)
	if err := os.MkdirAll(namespaceDir, 0755); err != nil {
		return fmt.Errorf("failed to create namespace directory: %w", err)
	}

	// Write to file
	file := filepath.Join(namespaceDir, key+".json")
	if err := os.WriteFile(file, []byte(value), 0644); err != nil {
		return fmt.Errorf("failed to write file: %w", err)
	}

	return nil
}

// KvDel deletes a value from general KV storage
func (t *TarfilesStore) KvDel(namespace string, key string) error {
	// Sanitize namespace and key to prevent directory traversal
	namespace, key, err := sanitizeNamespaceAndKey(namespace, key)
	if err != nil {
		return err
	}

	t.mu.Lock()
	defer t.mu.Unlock()

	// Build the file path
	filePath := filepath.Join(t.Dirpath, namespace, key+".json")

	// Remove the file
	err = os.Remove(filePath)
	if err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("failed to delete key file: %w", err)
	}

	return nil
}

// sanitizeNamespaceAndKey sanitizes namespace and key to make them safe for filesystem operations
func sanitizeNamespaceAndKey(namespace, key string) (string, string, error) {
	if namespace == "" {
		return "", "", fmt.Errorf("namespace cannot be empty")
	}
	if key == "" {
		return "", "", fmt.Errorf("key cannot be empty")
	}

	// Replace problematic characters with safe alternatives
	replacer := strings.NewReplacer(
		"..", "_", // Replace parent directory references
		"/", "_", // Replace forward slashes
		"\\", "_", // Replace backslashes
		"\x00", "_", // Replace null characters
		":", "_", // Replace colons (problematic on some filesystems)
		"*", "_", // Replace wildcards
		"?", "_", // Replace wildcards
		"\"", "_", // Replace quotes
		"<", "_", // Replace less than
		">", "_", // Replace greater than
		"|", "_", // Replace pipe
		"\n", "_", // Replace newlines
		"\r", "_", // Replace carriage returns
		"\t", "_", // Replace tabs
	)

	// Sanitize namespace and key
	namespace = replacer.Replace(namespace)
	key = replacer.Replace(key)

	// Ensure they don't start with a dot (hidden files)
	if strings.HasPrefix(namespace, ".") {
		namespace = "_" + namespace[1:]
	}
	if strings.HasPrefix(key, ".") {
		key = "_" + key[1:]
	}

	// Trim any leading/trailing spaces
	namespace = strings.TrimSpace(namespace)
	key = strings.TrimSpace(key)

	// Final check - make sure they're not empty after sanitization
	if namespace == "" {
		return "", "", fmt.Errorf("namespace became empty after sanitization")
	}
	if key == "" {
		return "", "", fmt.Errorf("key became empty after sanitization")
	}

	return namespace, key, nil
}
