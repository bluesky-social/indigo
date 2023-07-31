package querycheck

import (
	"context"
	"log"
	"math"
	"sync"
	"time"

	"github.com/jackc/pgx/v5"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.uber.org/zap"
)

var tracer = otel.Tracer("querycheck")

// Query is a query to check
type Query struct {
	Name        string
	Query       string
	LastPlan    *QueryPlan
	LastChecked time.Time
	LastError   error
	CheckEvery  time.Duration

	lk  sync.RWMutex
	in  chan struct{}
	out chan struct{}
}

// Querychecker is a query checker meta object
type Querychecker struct {
	Queries []*Query
	Logger  *zap.SugaredLogger

	connectionURL string
	lk            sync.RWMutex
}

// NewQuerychecker creates a new querychecker
func NewQuerychecker(ctx context.Context, connectionURL string) (*Querychecker, error) {
	logger, err := zap.NewDevelopment()
	if err != nil {
		return nil, err
	}
	l := logger.Sugar().With("source", "querychecker_manager")

	return &Querychecker{
		connectionURL: connectionURL,
		Logger:        l,
		Queries:       []*Query{},
	}, nil
}

// AddQuery adds a query to the checker
func (q *Querychecker) AddQuery(ctx context.Context, name, query string, checkEvery time.Duration) {
	ctx, span := tracer.Start(ctx, "AddQuery")
	defer span.End()

	span.SetAttributes(attribute.String("name", name))
	span.SetAttributes(attribute.String("query", query))
	span.SetAttributes(attribute.String("checkEvery", checkEvery.String()))

	q.lk.Lock()
	q.Queries = append(q.Queries, &Query{
		Name:       name,
		Query:      query,
		CheckEvery: checkEvery,

		in:  make(chan struct{}),
		out: make(chan struct{}),
	})
	q.lk.Unlock()
}

// RemoveQuery removes a query from the checker
func (q *Querychecker) RemoveQuery(ctx context.Context, name string) {
	ctx, span := tracer.Start(ctx, "RemoveQuery")
	defer span.End()

	span.SetAttributes(attribute.String("name", name))

	q.lk.Lock()
	defer q.lk.Unlock()
	for i, qu := range q.Queries {
		if qu.Name == name {
			q.Queries = append(q.Queries[:i], q.Queries[i+1:]...)
			return
		}
	}
}

// GetQuery returns a copy of the query
func (q *Querychecker) GetQuery(ctx context.Context, name string) *Query {
	ctx, span := tracer.Start(ctx, "GetQuery")
	defer span.End()

	span.SetAttributes(attribute.String("name", name))

	q.lk.RLock()
	defer q.lk.RUnlock()
	for _, qu := range q.Queries {
		if qu.Name == name {
			return &Query{
				Name:        qu.Name,
				Query:       qu.Query,
				LastPlan:    qu.LastPlan,
				LastChecked: qu.LastChecked,
				LastError:   qu.LastError,
				CheckEvery:  qu.CheckEvery,
			}
		}
	}
	return nil
}

// GetQueries returns a copy of the queries
func (q *Querychecker) GetQueries(ctx context.Context) []*Query {
	ctx, span := tracer.Start(ctx, "GetQueries")
	defer span.End()

	q.lk.RLock()
	defer q.lk.RUnlock()
	queries := make([]*Query, len(q.Queries))
	for i, qu := range q.Queries {
		queries[i] = &Query{
			Name:        qu.Name,
			Query:       qu.Query,
			LastPlan:    qu.LastPlan,
			LastChecked: qu.LastChecked,
			LastError:   qu.LastError,
			CheckEvery:  qu.CheckEvery,
		}
	}

	return queries
}

// UpdateQuery updates a query
func (q *Querychecker) UpdateQuery(ctx context.Context, name, query string, checkEvery time.Duration) {
	ctx, span := tracer.Start(ctx, "UpdateQuery")
	defer span.End()

	span.SetAttributes(attribute.String("name", name))
	span.SetAttributes(attribute.String("query", query))
	span.SetAttributes(attribute.String("checkEvery", checkEvery.String()))

	for _, qu := range q.Queries {
		if qu.Name == name {
			qu.lk.Lock()
			qu.Query = query
			qu.CheckEvery = checkEvery
			qu.lk.Unlock()
			return
		}
	}
}

// Start starts the query checker routines
func (q *Querychecker) Start() error {
	ctx, span := tracer.Start(context.Background(), "Start")
	defer span.End()

	for _, qu := range q.Queries {
		go func(query *Query) {
			rawlog, err := zap.NewDevelopment()
			if err != nil {
				log.Fatalf("failed to create logger: %+v\n", err)
			}
			log := rawlog.Sugar().With("source", "query_checker_routine", "query", query.Name)

			log.Infof("query checker routine started for query: %s\n", query.Name)
			log.Infof("Query: \n%s\n", query.Query)

			// Check the query plan every CheckEvery duration
			ticker := time.NewTicker(query.CheckEvery)
			defer ticker.Stop()

			query.LastPlan, err = q.CheckQueryPlan(ctx, query.Query)
			if err != nil {
				log.Errorf("failed to check query plan: %+v\n", err)
			}

			if query.LastPlan != nil {
				log.Infof("Initial plan:\n%+v\n", query.LastPlan.String())
				query.RecordPlanMetrics(*query.LastPlan)
				query.LastChecked = time.Now()
			}

			for {
				select {
				case <-ticker.C:
					log.Info("checking query plan")

					query.lk.RLock()
					queryString := query.Query
					query.lk.RUnlock()

					qp, err := q.CheckQueryPlan(ctx, queryString)

					query.lk.Lock()
					query.LastChecked = time.Now()
					query.LastError = err
					query.lk.Unlock()

					execCounter.WithLabelValues(query.Name).Inc()

					if err != nil || qp == nil {
						if qp == nil {
							log.Errorf("query plan is nil")
						}
						log.Errorf("failed to check query plan: %+v\n", err)
						errorCounter.WithLabelValues(query.Name).Inc()
						continue
					}

					query.lk.RLock()
					lastPlan := *query.LastPlan
					query.lk.RUnlock()

					query.RecordPlanMetrics(*qp)

					if !qp.HasSameStructureAs(lastPlan) {
						sign := "+"
						diff := math.Abs(lastPlan.Plan.ActualTotalTime - qp.Plan.ActualTotalTime)
						if lastPlan.Plan.ActualTotalTime > qp.Plan.ActualTotalTime {
							sign = "-"
						}

						log.Infof("query plan has changed (%s%.03fms): \n%+v\n", sign, diff, qp.String())

						query.lk.Lock()
						query.LastPlan = qp
						query.lk.Unlock()
					}
				case <-query.in:
					log.Info("shutting down query checker routine")
					query.out <- struct{}{}
					return
				}
			}
		}(qu)
	}

	return nil
}

// Stop stops the query checker routines
func (q *Querychecker) Stop() {
	_, span := tracer.Start(context.Background(), "Stop")
	defer span.End()

	q.Logger.Info("stopping query checker")

	for _, qu := range q.Queries {
		qu.in <- struct{}{}
	}

	for _, qu := range q.Queries {
		<-qu.out
	}

	q.Logger.Info("query checker stopped")
}

// CheckQueryPlan checks the query plan for a given query
func (q *Querychecker) CheckQueryPlan(ctx context.Context, query string) (*QueryPlan, error) {
	ctx, span := tracer.Start(ctx, "CheckQueryPlan")
	defer span.End()

	conn, err := pgx.Connect(ctx, q.connectionURL)
	if err != nil {
		return nil, err
	}
	defer conn.Close(ctx)

	rows, err := conn.Query(ctx, "EXPLAIN (ANALYZE, COSTS, VERBOSE, BUFFERS, FORMAT JSON) "+query)
	if err != nil {
		return nil, err
	}

	var plan QueryPlan

	for rows.Next() {
		var plans QueryPlans
		err := rows.Scan(&plans)
		if err != nil {
			return nil, err
		}
		for _, p := range plans {
			plan = p
		}
	}

	return &plan, nil
}

// RecordPlanMetrics records the query plan metrics
func (qu *Query) RecordPlanMetrics(qp QueryPlan) {
	execDurationCounter.WithLabelValues(qu.Name).Add(qp.Plan.ActualTotalTime)
	blocksHitCounter.WithLabelValues(qu.Name).Add(float64(qp.Plan.SharedHitBlocks))
	blocksReadCounter.WithLabelValues(qu.Name).Add(float64(qp.Plan.SharedReadBlocks))
	blocksWrittenCounter.WithLabelValues(qu.Name).Add(float64(qp.Plan.SharedWrittenBlocks))
	blocksDirtyCounter.WithLabelValues(qu.Name).Add(float64(qp.Plan.SharedDirtiedBlocks))
	ioReadTimeCounter.WithLabelValues(qu.Name).Add(qp.Plan.IOReadTime)
	ioWriteTimeCounter.WithLabelValues(qu.Name).Add(qp.Plan.IOWriteTime)
	tempWrittenBlocksCounter.WithLabelValues(qu.Name).Add(float64(qp.Plan.TempWrittenBlocks))

	qu.RecordPlanNode(qp.Plan)
}

// RecordPlanNode records the query plan node metrics
func (qu *Query) RecordPlanNode(p Plan) {
	planNodeCounter.WithLabelValues(qu.Name, p.NodeType).Inc()
	for _, n := range p.Plans {
		qu.RecordPlanNode(n)
	}
}
