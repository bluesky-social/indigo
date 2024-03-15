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
func (s *Server) UpdatePageranks(ctx context.Context, pagerankFile string) error {
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

	queue := make(chan pagerankJob, 1000)
	wg := &sync.WaitGroup{}

	// Start worker goroutines to process the pagerank jobs
	for i := int64(0); i < workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for job := range queue {
				if err := s.updateProfilePagerank(ctx, job.did, job.rank); err != nil {
					s.logger.Error("failed to update pagerank", "did", job.did, "rank", job.rank, "error", err)
				}
			}
		}()
	}

	// Iterate over each line in the file
	for scanner.Scan() {
		line := scanner.Text()

		linesRead++
		if linesRead%1000 == 0 {
			s.logger.Info("processed pagerank lines", "lines", linesRead)
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

		// Add the pagerank to the queue
		queue <- pagerankJob{did, rank}
	}

	// Close the queue and wait for all workers to finish
	close(queue)
	wg.Wait()

	// Check for any scanner errors
	if err := scanner.Err(); err != nil {
		return fmt.Errorf("error reading pagerank file: %w", err)
	}

	s.logger.Info("finished processing pagerank file", "lines", linesRead)

	return nil
}
