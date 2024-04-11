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
	"github.com/bluesky-social/indigo/atproto/identity"
	"github.com/bluesky-social/indigo/atproto/syntax"

	"github.com/ipfs/go-cid"
)

type pagerankJob struct {
	did  syntax.DID
	rank float64
}

// BulkIndexPageranks updates the pageranks for the DIDs in the Search Index from a CSV file.
func (idx *Indexer) BulkIndexPageranks(ctx context.Context, pagerankFile string) error {
	f, err := os.Open(pagerankFile)
	if err != nil {
		return fmt.Errorf("failed to open csv file: %w", err)
	}
	defer f.Close()

	// Run 5 pagerank indexers in parallel
	for i := 0; i < 5; i++ {
		go idx.runPagerankIndexer(ctx)
	}

	logger := idx.logger.With("source", "bulk_index_pageranks")

	queue := make(chan string, 20_000)
	wg := &sync.WaitGroup{}
	workerCount := 20
	for i := 0; i < workerCount; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for line := range queue {
				if err := idx.processPagerankCSVLine(line); err != nil {
					logger.Error("failed to process line", "err", err)
				}
			}
		}()
	}

	// Create a scanner to read the file line by line
	scanner := bufio.NewScanner(f)
	buf := make([]byte, 0, 64*1024)
	scanner.Buffer(buf, 1024*1024)

	linesRead := 0

	// Iterate over each line in the file
	for scanner.Scan() {
		line := scanner.Text()

		queue <- line

		linesRead++
		if linesRead%100_000 == 0 {
			idx.logger.Info("processed csv lines", "lines", linesRead)
		}
	}

	close(queue)

	// Check for any scanner errors
	if err := scanner.Err(); err != nil {
		return fmt.Errorf("error reading csv file: %w", err)
	}

	wg.Wait()

	idx.logger.Info("finished processing csv file", "lines", linesRead)

	return nil
}

// BulkIndexPosts indexes posts from a CSV file.
func (idx *Indexer) BulkIndexPosts(ctx context.Context, postsFile string) error {
	f, err := os.Open(postsFile)
	if err != nil {
		return fmt.Errorf("failed to open csv file: %w", err)
	}
	defer f.Close()

	// Run 5 post indexers in parallel
	for i := 0; i < 5; i++ {
		go idx.runPostIndexer(ctx)
	}

	logger := idx.logger.With("source", "bulk_index_posts")

	queue := make(chan string, 20_000)
	wg := &sync.WaitGroup{}
	workerCount := 20
	for i := 0; i < workerCount; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for line := range queue {
				if err := idx.processPostCSVLine(line); err != nil {
					logger.Error("failed to process line", "err", err)
				}
			}
		}()
	}

	// Create a scanner to read the file line by line
	scanner := bufio.NewScanner(f)
	buf := make([]byte, 0, 64*1024)
	scanner.Buffer(buf, 1024*1024)

	linesRead := 0

	// Iterate over each line in the file
	for scanner.Scan() {
		line := scanner.Text()

		queue <- line

		linesRead++
		if linesRead%100_000 == 0 {
			idx.logger.Info("processed csv lines", "lines", linesRead)
		}
	}

	close(queue)

	// Check for any scanner errors
	if err := scanner.Err(); err != nil {
		return fmt.Errorf("error reading csv file: %w", err)
	}

	wg.Wait()

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

	for i := 0; i < 5; i++ {
		go idx.runProfileIndexer(ctx)
	}

	logger := idx.logger.With("source", "bulk_index_profiles")

	queue := make(chan string, 20_000)
	wg := &sync.WaitGroup{}
	workerCount := 20
	for i := 0; i < workerCount; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for line := range queue {
				if err := idx.processProfileCSVLine(line); err != nil {
					logger.Error("failed to process line", "err", err)
				}
			}
		}()
	}

	// Create a scanner to read the file line by line
	scanner := bufio.NewScanner(f)
	buf := make([]byte, 0, 64*1024)
	scanner.Buffer(buf, 1024*1024)

	linesRead := 0

	// Iterate over each line in the file
	for scanner.Scan() {
		line := scanner.Text()

		queue <- line

		linesRead++
		if linesRead%100_000 == 0 {
			idx.logger.Info("processed csv lines", "lines", linesRead)
		}
	}

	close(queue)

	// Check for any scanner errors
	if err := scanner.Err(); err != nil {
		return fmt.Errorf("error reading csv file: %w", err)
	}

	wg.Wait()

	idx.logger.Info("finished processing csv file", "lines", linesRead)

	return nil
}

func (idx *Indexer) processPagerankCSVLine(line string) error {
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

	job := PagerankIndexJob{
		did:  did,
		rank: rank,
	}

	// Send the job to the pagerank queue
	idx.pagerankQueue <- &job

	return nil
}

func (idx *Indexer) processPostCSVLine(line string) error {
	// CSV is formatted as
	// actor_did,rkey,taken_down(time or null),violates_threadgate(False or null),cid,raw(post JSON as hex)
	parts := strings.Split(line, ",")
	if len(parts) != 6 {
		return fmt.Errorf("invalid csv line: %s", line)
	}

	did, err := syntax.ParseDID(parts[0])
	if err != nil {
		return fmt.Errorf("invalid DID: %s", parts[0])
	}

	rkey, err := syntax.ParseRecordKey(parts[1])
	if err != nil {
		return fmt.Errorf("invalid record key: %s", parts[1])
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
		return nil
	}

	cid, err := cid.Parse(parts[4])
	if err != nil {
		return fmt.Errorf("invalid CID: %s", parts[4])
	}

	if len(parts[5]) <= 2 {
		return nil
	}

	raw, err := hex.DecodeString(parts[5][2:])
	if err != nil {
		return fmt.Errorf("invalid raw record (%s/%s): %s", did, rkey, parts[5][2:])
	}

	post := appbsky.FeedPost{}
	if err := json.Unmarshal(raw, &post); err != nil {
		return fmt.Errorf("failed to unmarshal post: %w", err)
	}

	job := PostIndexJob{
		did:    did,
		rkey:   rkey.String(),
		rcid:   cid,
		record: &post,
	}

	// Send the job to the post queue
	idx.postQueue <- &job

	return nil
}

func (idx *Indexer) processProfileCSVLine(line string) error {
	// CSV is formatted as
	// actor_did,taken_down(time or null),cid,handle,raw(profile JSON as hex)
	parts := strings.Split(line, ",")
	if len(parts) != 5 {
		return fmt.Errorf("invalid csv line: %s", line)
	}

	did, err := syntax.ParseDID(parts[0])
	if err != nil {
		return fmt.Errorf("invalid DID: %s", parts[0])
	}

	isTakenDown := false
	if parts[1] != "" && parts[1] != "null" {
		isTakenDown = true
	}

	if isTakenDown {
		return nil
	}

	// Skip actors without profile records
	if parts[2] == "" {
		return nil
	}

	cid, err := cid.Parse(parts[2])
	if err != nil {
		return fmt.Errorf("invalid CID: %s", parts[2])
	}

	if len(parts[3]) <= 2 {
		return nil
	}

	raw, err := hex.DecodeString(parts[4][2:])
	if err != nil {
		return fmt.Errorf("invalid raw record (%s): %s", did, parts[4][2:])
	}

	profile := appbsky.ActorProfile{}
	if err := json.Unmarshal(raw, &profile); err != nil {
		return fmt.Errorf("failed to unmarshal profile: %w", err)
	}

	ident := identity.Identity{DID: did}

	handle, err := syntax.ParseHandle(parts[3])
	if err != nil {
		ident.Handle = syntax.HandleInvalid
	} else {
		ident.Handle = handle
	}

	job := ProfileIndexJob{
		ident:  &ident,
		rcid:   cid,
		record: &profile,
	}

	// Send the job to the profile queue
	idx.profileQueue <- &job

	return nil
}
