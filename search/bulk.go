package search

import (
	"bufio"
	"context"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"strconv"
	"strings"
	"sync"

	appbsky "github.com/bluesky-social/indigo/api/bsky"
	"github.com/bluesky-social/indigo/atproto/syntax"
	"github.com/ipfs/go-cid"
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

// BulkIndexPosts indexes posts from a CSV file.
func (idx *Indexer) BulkIndexPosts(ctx context.Context, postsFile string) error {
	f, err := os.Open(postsFile)
	if err != nil {
		return fmt.Errorf("failed to open csv file: %w", err)
	}
	defer f.Close()

	// Create a scanner to read the file line by line
	scanner := bufio.NewScanner(f)

	linesRead := 0
	logger := idx.logger.With("source", "bulk_index_posts")

	// Iterate over each line in the file
	for scanner.Scan() {
		line := scanner.Text()

		linesRead++
		if linesRead%1000 == 0 {
			idx.logger.Info("processed csv lines", "lines", linesRead)
		}

		// CSV is formatted as
		// actor_did,rkey,taken_down(time or null),violates_threadgate(False or null),cid,raw(post JSON as hex)
		parts := strings.Split(line, ",")
		if len(parts) != 6 {
			logger.Error("invalid csv line", "line", line)
			continue
		}

		did, err := syntax.ParseDID(parts[0])
		if err != nil {
			logger.Error("invalid DID", "did", parts[0])
			continue
		}

		rkey, err := syntax.ParseRecordKey(parts[1])
		if err != nil {
			logger.Error("invalid rkey", "rkey", parts[1])
			continue
		}

		isTakenDown := false
		if parts[2] != "" && parts[2] != "null" {
			isTakenDown = true
		}

		violatesThreadgate := false
		if parts[3] != "" && parts[3] != "False" {
			violatesThreadgate = true
		}

		if isTakenDown || violatesThreadgate {
			continue
		}

		cid, err := cid.Parse(parts[4])
		if err != nil {
			logger.Error("invalid CID", "cid", parts[4])
			continue
		}

		raw, err := hex.DecodeString(parts[5])
		if err != nil {
			logger.Error("invalid raw post", "raw", parts[5])
			continue
		}

		post := appbsky.FeedPost{}
		if err := json.Unmarshal(raw, &post); err != nil {
			logger.Error("failed to unmarshal post", "err", err, "did", did, "rkey", rkey)
			continue
		}

		// Lookup the profile for the identity for the actor
		ident, err := idx.dir.LookupDID(ctx, did)
		if err != nil {
			logger.Error("failed to lookup identity", "err", err, "did", did)
			continue
		}

		job := PostIndexJob{
			ident:  ident,
			rkey:   rkey.String(),
			rcid:   cid,
			record: &post,
		}

		// Send the job to the post queue
		idx.postQueue <- &job
	}

	// Check for any scanner errors
	if err := scanner.Err(); err != nil {
		return fmt.Errorf("error reading csv file: %w", err)
	}

	idx.logger.Info("finished processing csv file", "lines", linesRead)

	return nil
}

// BulkIndexProfiles indexes profiles from a CSV file.
func (idx *Indexer) BulkIndexProfiles(ctx context.Context, profilesFile string) error {
	f, err := os.Open(profilesFile)
	if err != nil {
		return fmt.Errorf("failed to open csv file: %w", err)
	}
	defer f.Close()

	// Create a scanner to read the file line by line
	scanner := bufio.NewScanner(f)

	linesRead := 0
	logger := idx.logger.With("source", "bulk_index_profiles")

	// Iterate over each line in the file
	for scanner.Scan() {
		line := scanner.Text()

		linesRead++
		if linesRead%1000 == 0 {
			idx.logger.Info("processed csv lines", "lines", linesRead)
		}

		// CSV is formatted as
		// actor_did,taken_down(time or null),cid,raw(profile JSON as hex)
		parts := strings.Split(line, ",")
		if len(parts) != 4 {
			logger.Error("invalid csv line", "line", line)
			continue
		}

		did, err := syntax.ParseDID(parts[0])
		if err != nil {
			logger.Error("invalid DID", "did", parts[0])
			continue
		}

		isTakenDown := false
		if parts[1] != "" && parts[1] != "null" {
			isTakenDown = true
		}

		if isTakenDown {
			continue
		}

		cid, err := cid.Parse(parts[2])
		if err != nil {
			logger.Error("invalid CID", "cid", parts[4])
			continue
		}

		raw, err := hex.DecodeString(parts[3])
		if err != nil {
			logger.Error("invalid raw profile", "raw", parts[5])
			continue
		}

		profile := appbsky.ActorProfile{}
		if err := json.Unmarshal(raw, &profile); err != nil {
			logger.Error("failed to unmarshal profile", "err", err, "did", did)
			continue
		}

		// Lookup the identity for the actor
		ident, err := idx.dir.LookupDID(ctx, did)
		if err != nil {
			logger.Error("failed to lookup identity", "err", err, "did", did)
			continue
		}

		job := ProfileIndexJob{
			ident:  ident,
			rcid:   cid,
			record: &profile,
		}

		// Send the job to the profile queue
		idx.profileQueue <- &job
	}

	// Check for any scanner errors
	if err := scanner.Err(); err != nil {
		return fmt.Errorf("error reading csv file: %w", err)
	}

	idx.logger.Info("finished processing csv file", "lines", linesRead)

	return nil
}
