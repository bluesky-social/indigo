package main

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/gocql/gocql"
	"github.com/scylladb/gocqlx/v2"
	"github.com/scylladb/gocqlx/v2/qb"
	"github.com/urfave/cli/v2"
	"golang.org/x/sync/semaphore"
	"golang.org/x/text/language"
	"golang.org/x/text/message"
)

func GetPostsForUser(cctx *cli.Context) error {
	ctx := cctx.Context
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	// cluster := gocql.NewCluster(cctx.StringSlice("scylla-nodes")...)
	// session, err := gocqlx.WrapSession(cluster.CreateSession())
	// if err != nil {
	// 	return fmt.Errorf("failed to create scylla session: %w", err)
	// }

	// args := cctx.Args()
	// if args.Len() != 1 {
	// 	return fmt.Errorf("must provide a did")
	// }

	// did := args.First()

	// limit := 500

	// numRuns := 2000
	// maxConcurrent := 40
	// sem := semaphore.NewWeighted(int64(maxConcurrent))

	// totalRowsRead := atomic.Uint64{}

	// runtimes := make(chan time.Duration, numRuns)

	// start := time.Now()

	return nil
}

func Trim(cctx *cli.Context) error {
	ctx := cctx.Context
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	limit := uint(20_001)

	// Read repo list
	repoListFile, err := os.Open(cctx.String("repo-list"))
	if err != nil {
		return err
	}

	fileScanner := bufio.NewScanner(repoListFile)
	fileScanner.Split(bufio.ScanLines)

	repos := []string{}

	for fileScanner.Scan() {
		repo := fileScanner.Text()
		repos = append(repos, repo)
	}

	cluster := gocql.NewCluster(cctx.StringSlice("scylla-nodes")...)
	session, err := gocqlx.WrapSession(cluster.CreateSession())
	if err != nil {
		return fmt.Errorf("failed to create scylla session: %w", err)
	}

	// Trim posts_by_did to 20,000 posts
	// Select 20k posts, grab the `created_at` of the last one, then delete all posts with a `created_at` less than that

	type output struct {
		err      error
		msg      string
		duration time.Duration
	}

	st := time.Now()
	outChan := make(chan output, len(repos))
	sem := semaphore.NewWeighted(int64(cctx.Int("worker-count")))
	var wg sync.WaitGroup

	// Run in parallel
	for _, repo := range repos {
		wg.Add(1)
		go func(repo string) {
			defer wg.Done()
			sem.Acquire(ctx, 1)
			defer sem.Release(1)

			start := time.Now()
			log := log.With("repo", repo)

			posts := []PostByDID{}
			err = postsByDIDTable.SelectBuilder().
				Limit(limit).
				OrderBy("created_at", qb.DESC).
				QueryContext(ctx, session).
				BindStruct(&PostByDID{Did: repo}).
				SelectRelease(&posts)
			if err != nil {
				outChan <- output{err: fmt.Errorf("failed to get posts: %w", err)}
				return
			}

			if len(posts) == 0 {
				outChan <- output{msg: "no posts found for DID", duration: time.Since(start)}
				return
			}

			loadTime := time.Now()

			log.Debugw("got posts", "num_posts", len(posts), "duration", loadTime.Sub(start).String())

			if len(posts) < int(limit) {
				outChan <- output{msg: "no posts to trim", duration: time.Since(start)}
				return
			}

			// Get the last post's created_at
			lastPostCreatedAt := posts[len(posts)-2].CreatedAt

			// Delete all posts with a created_at less than the last post's created_at
			err = qb.Delete(postsByDIDTable.Name()).
				Where(qb.Eq("did"), qb.Lt("created_at")).
				QueryContext(ctx, session).
				BindStruct(&PostByDID{Did: repo, CreatedAt: lastPostCreatedAt}).
				ExecRelease()
			if err != nil {
				outChan <- output{err: fmt.Errorf("failed to delete posts: %w", err)}
				return
			}

			deleteTime := time.Now()

			log.Debugw("deleted posts", "duration", deleteTime.Sub(loadTime).String())

			outChan <- output{msg: "trimmed posts", duration: time.Since(start)}
		}(repo)
	}

	// Wait for all the queries to finish
	wg.Wait()
	close(outChan)

	end := time.Now()

	// Enumerate the results
	var total time.Duration
	errs := []error{}
	successes := 0
	for out := range outChan {
		if out.err != nil {
			errs = append(errs, out.err)
		}
		if out.duration > 0 {
			total += out.duration
			successes++
		}
		if out.msg != "" {
			log.Debug(out.msg)
		}
	}

	// Calculate the average runtime
	avg := total / time.Duration(successes)

	p := message.NewPrinter(language.English)

	log.Info(p.Sprintf("trimmed %d repos (%d errors) in %s (avg: %s, total: %s)", successes, len(errs), end.Sub(st), avg, total))

	return nil
}

func Query(cctx *cli.Context) error {
	ctx := cctx.Context
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	cluster := gocql.NewCluster(cctx.StringSlice("scylla-nodes")...)
	session, err := gocqlx.WrapSession(cluster.CreateSession())
	if err != nil {
		return fmt.Errorf("failed to create scylla session: %w", err)
	}

	args := cctx.Args()
	if args.Len() != 1 {
		return fmt.Errorf("must provide a post URI")
	}
	postURI := args.First()

	// at://did/app.bsky.feed.post/rkey
	postURI = strings.TrimPrefix(postURI, "at://")
	postParts := strings.Split(postURI, "/")
	if len(postParts) != 3 {
		return fmt.Errorf("invalid post URI: %s", postURI)
	}

	numRuns := 500000
	maxConcurrent := 400
	sem := semaphore.NewWeighted(int64(maxConcurrent))

	totalRowsRead := atomic.Uint64{}

	runtimes := make(chan time.Duration, numRuns)

	start := time.Now()

	// Run the query in numRuns goroutines
	var pwg sync.WaitGroup
	for i := 0; i < numRuns; i++ {
		pwg.Add(1)
		go func() error {
			defer pwg.Done()
			sem.Acquire(ctx, 1)
			defer sem.Release(1)
			iterStart := time.Now()
			defer func() {
				runtimes <- time.Since(iterStart)
			}()

			// Get the post
			post := Post{
				Did:  postParts[0],
				Rkey: postParts[2],
			}
			err = postTable.GetQuery(session).BindStruct(&post).GetRelease(&post)
			if err != nil {
				return fmt.Errorf("failed to get post: %w", err)
			}

			totalRowsRead.Add(1)

			// Get the replies
			replyRefs := []Reply{}
			err = repliesTable.SelectQuery(session).BindStruct(&Reply{
				ParentDid:  postParts[0],
				ParentRkey: postParts[2],
			}).SelectRelease(&replyRefs)
			if err != nil {
				return fmt.Errorf("failed to get replies: %w", err)
			}

			totalRowsRead.Add(uint64(len(replyRefs)))

			replies := []Post{}
			lk := sync.Mutex{}

			// Resolve the replies as posts in parallel
			var wg sync.WaitGroup
			for i := range replyRefs {
				wg.Add(1)
				replyRef := replyRefs[i]
				go func(replyRef Reply) {
					defer wg.Done()

					reply := Post{
						Did:  replyRef.ChildDid,
						Rkey: replyRef.ChildRkey,
					}

					err = postTable.GetQuery(session).BindStruct(&reply).GetRelease(&reply)
					if err != nil {
						log.Errorf("failed to get reply: %+v", err)
						return
					}
					lk.Lock()
					replies = append(replies, reply)
					lk.Unlock()
				}(replyRef)
			}

			totalRowsRead.Add(uint64(len(replies)))

			// Resolve the parent up to the root
			parents := []Post{}
			if post.ParentDid != "" && post.ParentRkey != "" {
				wg.Add(1)
				go func() {
					defer wg.Done()
					parentDid := post.ParentDid
					parentRkey := post.ParentRkey
					for {
						parent := Post{
							Did:  parentDid,
							Rkey: parentRkey,
						}
						err = postTable.GetQuery(session).BindStruct(&parent).GetRelease(&parent)
						if err != nil && err != gocql.ErrNotFound {
							log.Errorf("failed to get parent: %+v", err)
							return
						}

						parents = append(parents, parent)

						if parent.ParentDid == "" {
							break
						}

						parentDid = parent.ParentDid
						parentRkey = parent.ParentRkey
					}
				}()
			}

			totalRowsRead.Add(uint64(len(parents)))

			wg.Wait()
			return nil
		}()
	}

	// // Print the thread
	// p := message.NewPrinter(language.English)
	// log.Debugf("post: %s", post.Content)
	// log.Debugf("replies: %d", len(replies))
	// for _, reply := range replies {
	// 	log.Debugf("  %s", reply.Content)
	// }
	// slices.Reverse(parents)
	// log.Debugf("parents: %d", len(parents))
	// for _, parent := range parents {
	// 	log.Debugf("  %s", parent.Content)
	// }

	// log.Info(p.Sprintf("processed post with %d replies and resolved %d parents in %s", len(replies), len(parents), time.Since(start)))

	// Wait for all the queries to finish
	pwg.Wait()
	clockTime := time.Since(start)
	close(runtimes)

	// Calculate the average runtime
	var total time.Duration
	for runtime := range runtimes {
		total += runtime
	}
	avg := total / time.Duration(numRuns)

	p := message.NewPrinter(language.English)

	log.Info(p.Sprintf("processed post %d times (%d total reads) in %s (avg: %s, total: %s)", numRuns, totalRowsRead.Load(), clockTime, avg, total))

	return nil
}
