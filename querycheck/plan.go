package querycheck

import "fmt"

type Plan struct {
	NodeType            string   `json:"Node Type"`
	ParallelAware       bool     `json:"Parallel Aware"`
	AsyncCapable        bool     `json:"Async Capable"`
	StartupCost         float64  `json:"Startup Cost"`
	TotalCost           float64  `json:"Total Cost"`
	PlanRows            int      `json:"Plan Rows"`
	PlanWidth           int      `json:"Plan Width"`
	ActualStartupTime   float64  `json:"Actual Startup Time"`
	ActualTotalTime     float64  `json:"Actual Total Time"`
	ActualRows          int      `json:"Actual Rows"`
	ActualLoops         int      `json:"Actual Loops"`
	Output              []string `json:"Output"`
	SharedHitBlocks     int      `json:"Shared Hit Blocks"`
	SharedReadBlocks    int      `json:"Shared Read Blocks"`
	SharedDirtiedBlocks int      `json:"Shared Dirtied Blocks"`
	SharedWrittenBlocks int      `json:"Shared Written Blocks"`
	LocalHitBlocks      int      `json:"Local Hit Blocks"`
	LocalReadBlocks     int      `json:"Local Read Blocks"`
	LocalDirtiedBlocks  int      `json:"Local Dirtied Blocks"`
	LocalWrittenBlocks  int      `json:"Local Written Blocks"`
	TempReadBlocks      int      `json:"Temp Read Blocks"`
	TempWrittenBlocks   int      `json:"Temp Written Blocks"`
	IOReadTime          float64  `json:"I/O Read Time"`
	IOWriteTime         float64  `json:"I/O Write Time"`
	Plans               []Plan   `json:"Plans,omitempty"`
	ParentRelationship  string   `json:"Parent Relationship,omitempty"`
	SortKey             []string `json:"Sort Key,omitempty"`
	SortMethod          string   `json:"Sort Method,omitempty"`
	SortSpaceUsed       int      `json:"Sort Space Used,omitempty"`
	SortSpaceType       string   `json:"Sort Space Type,omitempty"`
	WorkersPlanned      int      `json:"Workers Planned,omitempty"`
	WorkersLaunched     int      `json:"Workers Launched,omitempty"`
	SingleCopy          bool     `json:"Single Copy,omitempty"`
	RelationName        string   `json:"Relation Name,omitempty"`
	Schema              string   `json:"Schema,omitempty"`
	Alias               string   `json:"Alias,omitempty"`
	Filter              string   `json:"Filter,omitempty"`
	RowsRemovedByFilter int      `json:"Rows Removed by Filter,omitempty"`
	Workers             []Worker `json:"Workers,omitempty"`
}

type Worker struct {
	WorkerNumber        int     `json:"Worker Number"`
	ActualStartupTime   float64 `json:"Actual Startup Time"`
	ActualTotalTime     float64 `json:"Actual Total Time"`
	ActualRows          int     `json:"Actual Rows"`
	ActualLoops         int     `json:"Actual Loops"`
	JIT                 JIT     `json:"JIT"`
	SharedHitBlocks     int     `json:"Shared Hit Blocks"`
	SharedReadBlocks    int     `json:"Shared Read Blocks"`
	SharedDirtiedBlocks int     `json:"Shared Dirtied Blocks"`
	SharedWrittenBlocks int     `json:"Shared Written Blocks"`
	LocalHitBlocks      int     `json:"Local Hit Blocks"`
	LocalReadBlocks     int     `json:"Local Read Blocks"`
	LocalDirtiedBlocks  int     `json:"Local Dirtied Blocks"`
	LocalWrittenBlocks  int     `json:"Local Written Blocks"`
	TempReadBlocks      int     `json:"Temp Read Blocks"`
	TempWrittenBlocks   int     `json:"Temp Written Blocks"`
	IOReadTime          float64 `json:"I/O Read Time"`
	IOWriteTime         float64 `json:"I/O Write Time"`
}

type JIT struct {
	Functions int     `json:"Functions"`
	Options   Options `json:"Options"`
	Timing    Timing  `json:"Timing"`
}

type Options struct {
	Inlining     bool `json:"Inlining"`
	Optimization bool `json:"Optimization"`
	Expressions  bool `json:"Expressions"`
	Deforming    bool `json:"Deforming"`
}

type Timing struct {
	Generation   float64 `json:"Generation"`
	Inlining     float64 `json:"Inlining"`
	Optimization float64 `json:"Optimization"`
	Emission     float64 `json:"Emission"`
	Total        float64 `json:"Total"`
}

type QueryPlan struct {
	Plan            Plan     `json:"Plan"`
	QueryIdentifier int64    `json:"Query Identifier"`
	Planning        Planning `json:"Planning"`
	PlanningTime    float64  `json:"Planning Time"`
	Triggers        []string `json:"Triggers"`
	JIT             JIT      `json:"JIT"`
	ExecutionTime   float64  `json:"Execution Time"`
}

type Planning struct {
	SharedHitBlocks     int     `json:"Shared Hit Blocks"`
	SharedReadBlocks    int     `json:"Shared Read Blocks"`
	SharedDirtiedBlocks int     `json:"Shared Dirtied Blocks"`
	SharedWrittenBlocks int     `json:"Shared Written Blocks"`
	LocalHitBlocks      int     `json:"Local Hit Blocks"`
	LocalReadBlocks     int     `json:"Local Read Blocks"`
	LocalDirtiedBlocks  int     `json:"Local Dirtied Blocks"`
	LocalWrittenBlocks  int     `json:"Local Written Blocks"`
	TempReadBlocks      int     `json:"Temp Read Blocks"`
	TempWrittenBlocks   int     `json:"Temp Written Blocks"`
	IOReadTime          float64 `json:"I/O Read Time"`
	IOWriteTime         float64 `json:"I/O Write Time"`
}

type QueryPlans []QueryPlan

func (q *QueryPlan) String() string {
	ret := ""
	ret += q.Plan.String(1)
	return ret
}

func (p *Plan) String(i int) string {
	ret := ""
	ret += fmt.Sprintf("(%s) Timing: %fms | IO Read: %fms (H %d R %d D %d W %d) | IO Write: %fms",
		p.NodeType,
		p.ActualTotalTime,
		p.IOReadTime,
		p.SharedHitBlocks,
		p.SharedReadBlocks,
		p.SharedWrittenBlocks,
		p.SharedDirtiedBlocks,
		p.IOWriteTime,
	)

	for _, plan := range p.Plans {
		ret += "\n"
		for j := 0; j < i; j++ {
			ret += "\t"
		}
		ret += plan.String(i + 1)
	}

	return ret
}

func (q *QueryPlan) HasSameStructureAs(other QueryPlan) bool {
	return q.Plan.HasSameStructureAs(other.Plan)
}

func (p *Plan) HasSameStructureAs(other Plan) bool {
	if p.NodeType != other.NodeType {
		return false
	}

	if len(p.Plans) != len(other.Plans) {
		return false
	}

	for i, plan := range p.Plans {
		if !plan.HasSameStructureAs(other.Plans[i]) {
			return false
		}
	}

	return true
}
