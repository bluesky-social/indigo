package search

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"strconv"
	"strings"
	"sync"

	"github.com/bluesky-social/indigo/atproto/syntax"
)

type pagerankJob struct {
	did  syntax.DID
	rank float64
}

// UpdatePageranks updates the pageranks for the DIDs in the Serch Index from a CSV file.
func (idx *Indexer) UpdatePageranks(ctx context.Context, pagerankFile string) error {
	// Open the pagerank CSV file and read the pageranks into a map
	f, err := os.Open(pagerankFile)
	if err != nil {
		return fmt.Errorf("failed to open pagerank file: %w", err)
	}
	defer f.Close()

	// Create a scanner to read the file line by line
	scanner := bufio.NewScanner(f)

	linesRead := 0
	workers := int64(20)

	queue := make(chan []pagerankJob, workers)
	wg := &sync.WaitGroup{}

	// Start worker goroutines to process the pagerank jobs
	for i := int64(0); i < workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for jobChunk := range queue {
				dids := make([]syntax.DID, len(jobChunk))
				ranks := make([]float64, len(jobChunk))
				for i, job := range jobChunk {
					dids[i] = job.did
					ranks[i] = job.rank
				}
				if err := idx.updateProfilePageranks(ctx, dids, ranks); err != nil {
					idx.logger.Error("failed to update pageranks", "err", err)
				}
			}
		}()
	}

	var jobs []pagerankJob
	batchSize := 1000

	// Iterate over each line in the file
	for scanner.Scan() {
		line := scanner.Text()

		linesRead++
		if linesRead%1000 == 0 {
			idx.logger.Info("processed pagerank lines", "lines", linesRead)
		}

		// Split the line into DID and rank
		parts := strings.Split(line, ",")
		if len(parts) != 2 {
			return fmt.Errorf("invalid pagerank line: %s", line)
		}

		did, err := syntax.ParseDID(parts[0])
		if err != nil {
			return fmt.Errorf("invalid DID: %s", parts[0])
		}

		rank, err := strconv.ParseFloat(parts[1], 64)
		if err != nil {
			return fmt.Errorf("invalid pagerank value: %s", parts[1])
		}

		// Add the pagerank job to the batch
		jobs = append(jobs, pagerankJob{did, rank})

		// If the batch size is reached, send the jobs to the queue
		if len(jobs) == batchSize {
			queue <- jobs
			jobs = nil
		}
	}

	// Send any remaining jobs to the queue
	if len(jobs) > 0 {
		queue <- jobs
	}

	// Close the queue and wait for all workers to finish
	close(queue)
	wg.Wait()

	// Check for any scanner errors
	if err := scanner.Err(); err != nil {
		return fmt.Errorf("error reading pagerank file: %w", err)
	}

	idx.logger.Info("finished processing pagerank file", "lines", linesRead)

	return nil
}
