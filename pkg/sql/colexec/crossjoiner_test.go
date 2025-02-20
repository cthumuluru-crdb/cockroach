// Copyright 2018 The Cockroach Authors.
//
// Use of this software is governed by the CockroachDB Software License
// included in the /LICENSE file.

package colexec

import (
	"context"
	"fmt"
	"testing"

	"github.com/cockroachdb/cockroach/pkg/settings/cluster"
	"github.com/cockroachdb/cockroach/pkg/sql/catalog/descpb"
	"github.com/cockroachdb/cockroach/pkg/sql/colexec/colexecargs"
	"github.com/cockroachdb/cockroach/pkg/sql/colexec/colexectestutils"
	"github.com/cockroachdb/cockroach/pkg/sql/colexecop"
	"github.com/cockroachdb/cockroach/pkg/sql/execinfra"
	"github.com/cockroachdb/cockroach/pkg/sql/execinfrapb"
	"github.com/cockroachdb/cockroach/pkg/sql/sem/eval"
	"github.com/cockroachdb/cockroach/pkg/sql/types"
	"github.com/cockroachdb/cockroach/pkg/testutils/colcontainerutils"
	"github.com/cockroachdb/cockroach/pkg/util/leaktest"
	"github.com/cockroachdb/cockroach/pkg/util/log"
	"github.com/stretchr/testify/require"
)

func getCJTestCases() []*joinTestCase {
	cjTestCases := []*joinTestCase{
		{
			description:  "inner join, cross product",
			leftTypes:    []*types.T{types.Int},
			rightTypes:   []*types.T{types.Int},
			leftTuples:   colexectestutils.Tuples{{0}, {1}, {2}},
			rightTuples:  colexectestutils.Tuples{{3}, {4}},
			leftOutCols:  []uint32{0},
			rightOutCols: []uint32{0},
			joinType:     descpb.InnerJoin,
			expected:     colexectestutils.Tuples{{0, 3}, {0, 4}, {1, 3}, {1, 4}, {2, 3}, {2, 4}},
		},
		{
			description:  "inner join with ON expression, cross product",
			leftTypes:    []*types.T{types.Int},
			rightTypes:   []*types.T{types.Int},
			leftTuples:   colexectestutils.Tuples{{0}, {nil}, {1}, {2}},
			rightTuples:  colexectestutils.Tuples{{0}, {3}, {4}, {nil}},
			leftOutCols:  []uint32{0},
			rightOutCols: []uint32{0},
			joinType:     descpb.InnerJoin,
			onExpr:       execinfrapb.Expression{Expr: "@1 + @2 > 1 AND @1 + @2 < 5"},
			expected:     colexectestutils.Tuples{{0, 3}, {0, 4}, {1, 3}, {2, 0}},
		},
		{
			description:  "inner join, left empty",
			leftTypes:    []*types.T{types.Int},
			rightTypes:   []*types.T{types.Int},
			leftTuples:   colexectestutils.Tuples{},
			rightTuples:  colexectestutils.Tuples{{3}, {4}},
			leftOutCols:  []uint32{0},
			rightOutCols: []uint32{0},
			joinType:     descpb.InnerJoin,
			// Injecting nulls into the right input won't change the output.
			skipAllNullsInjection: true,
			expected:              colexectestutils.Tuples{},
		},
		{
			description:  "inner join, right empty",
			leftTypes:    []*types.T{types.Int},
			rightTypes:   []*types.T{types.Int},
			leftTuples:   colexectestutils.Tuples{{0}, {1}, {2}},
			rightTuples:  colexectestutils.Tuples{},
			leftOutCols:  []uint32{0},
			rightOutCols: []uint32{0},
			joinType:     descpb.InnerJoin,
			// Injecting nulls into the left input won't change the output.
			skipAllNullsInjection: true,
			expected:              colexectestutils.Tuples{},
		},
		{
			description:  "left outer join, cross product",
			leftTypes:    []*types.T{types.Int},
			rightTypes:   []*types.T{types.Int},
			leftTuples:   colexectestutils.Tuples{{0}, {1}, {2}},
			rightTuples:  colexectestutils.Tuples{{3}, {4}},
			leftOutCols:  []uint32{0},
			rightOutCols: []uint32{0},
			joinType:     descpb.LeftOuterJoin,
			expected:     colexectestutils.Tuples{{0, 3}, {0, 4}, {1, 3}, {1, 4}, {2, 3}, {2, 4}},
		},
		{
			description:  "left outer join, left empty",
			leftTypes:    []*types.T{types.Int},
			rightTypes:   []*types.T{types.Int},
			leftTuples:   colexectestutils.Tuples{},
			rightTuples:  colexectestutils.Tuples{{3}, {4}},
			leftOutCols:  []uint32{0},
			rightOutCols: []uint32{0},
			joinType:     descpb.LeftOuterJoin,
			// Injecting nulls into the right input won't change the output.
			skipAllNullsInjection: true,
			expected:              colexectestutils.Tuples{},
		},
		{
			description:  "left outer join, right empty",
			leftTypes:    []*types.T{types.Int},
			rightTypes:   []*types.T{types.Int},
			leftTuples:   colexectestutils.Tuples{{0}, {1}, {2}, {3}},
			rightTuples:  colexectestutils.Tuples{},
			leftOutCols:  []uint32{0},
			rightOutCols: []uint32{0},
			joinType:     descpb.LeftOuterJoin,
			expected:     colexectestutils.Tuples{{0, nil}, {1, nil}, {2, nil}, {3, nil}},
		},
		{
			description:  "right outer join, cross product",
			leftTypes:    []*types.T{types.Int},
			rightTypes:   []*types.T{types.Int},
			leftTuples:   colexectestutils.Tuples{{0}, {1}, {2}},
			rightTuples:  colexectestutils.Tuples{{3}, {4}},
			leftOutCols:  []uint32{0},
			rightOutCols: []uint32{0},
			joinType:     descpb.RightOuterJoin,
			expected:     colexectestutils.Tuples{{0, 3}, {0, 4}, {1, 3}, {1, 4}, {2, 3}, {2, 4}},
		},
		{
			description:  "right outer join, left empty",
			leftTypes:    []*types.T{types.Int},
			rightTypes:   []*types.T{types.Int},
			leftTuples:   colexectestutils.Tuples{},
			rightTuples:  colexectestutils.Tuples{{1}, {3}, {3}, {4}},
			leftOutCols:  []uint32{0},
			rightOutCols: []uint32{0},
			joinType:     descpb.RightOuterJoin,
			expected:     colexectestutils.Tuples{{nil, 1}, {nil, 3}, {nil, 3}, {nil, 4}},
		},
		{
			description:  "right outer join, right empty",
			leftTypes:    []*types.T{types.Int},
			rightTypes:   []*types.T{types.Int},
			leftTuples:   colexectestutils.Tuples{{0}, {1}, {2}, {3}},
			rightTuples:  colexectestutils.Tuples{},
			leftOutCols:  []uint32{0},
			rightOutCols: []uint32{0},
			joinType:     descpb.RightOuterJoin,
			// Injecting nulls into the left input won't change the output.
			skipAllNullsInjection: true,
			expected:              colexectestutils.Tuples{},
		},
		{
			description:  "full outer join, cross product",
			leftTypes:    []*types.T{types.Int},
			rightTypes:   []*types.T{types.Int},
			leftTuples:   colexectestutils.Tuples{{0}, {1}, {2}},
			rightTuples:  colexectestutils.Tuples{{3}, {4}},
			leftOutCols:  []uint32{0},
			rightOutCols: []uint32{0},
			joinType:     descpb.FullOuterJoin,
			expected:     colexectestutils.Tuples{{0, 3}, {0, 4}, {1, 3}, {1, 4}, {2, 3}, {2, 4}},
		},
		{
			description:  "full outer join, left empty",
			leftTypes:    []*types.T{types.Int},
			rightTypes:   []*types.T{types.Int},
			leftTuples:   colexectestutils.Tuples{},
			rightTuples:  colexectestutils.Tuples{{1}, {3}, {3}, {4}},
			leftOutCols:  []uint32{0},
			rightOutCols: []uint32{0},
			joinType:     descpb.FullOuterJoin,
			expected:     colexectestutils.Tuples{{nil, 1}, {nil, 3}, {nil, 3}, {nil, 4}},
		},
		{
			description:  "full outer join, right empty",
			leftTypes:    []*types.T{types.Int},
			rightTypes:   []*types.T{types.Int},
			leftTuples:   colexectestutils.Tuples{{0}, {1}, {2}, {3}},
			rightTuples:  colexectestutils.Tuples{},
			leftOutCols:  []uint32{0},
			rightOutCols: []uint32{0},
			joinType:     descpb.FullOuterJoin,
			expected:     colexectestutils.Tuples{{0, nil}, {1, nil}, {2, nil}, {3, nil}},
		},
		{
			description: "left semi join, right non-empty",
			leftTypes:   []*types.T{types.Int},
			rightTypes:  []*types.T{types.Int},
			leftTuples:  colexectestutils.Tuples{{0}, {1}, {2}},
			rightTuples: colexectestutils.Tuples{{3}, {4}},
			leftOutCols: []uint32{0},
			joinType:    descpb.LeftSemiJoin,
			expected:    colexectestutils.Tuples{{0}, {1}, {2}},
		},
		{
			description: "left semi join, left empty",
			leftTypes:   []*types.T{types.Int},
			rightTypes:  []*types.T{types.Int},
			leftTuples:  colexectestutils.Tuples{},
			rightTuples: colexectestutils.Tuples{{3}, {4}},
			leftOutCols: []uint32{0},
			joinType:    descpb.LeftSemiJoin,
			// Injecting nulls into the right input won't change the output.
			skipAllNullsInjection: true,
			expected:              colexectestutils.Tuples{},
		},
		{
			description: "left semi join, right empty",
			leftTypes:   []*types.T{types.Int},
			rightTypes:  []*types.T{types.Int},
			leftTuples:  colexectestutils.Tuples{{0}, {1}, {2}, {3}},
			rightTuples: colexectestutils.Tuples{},
			leftOutCols: []uint32{0},
			joinType:    descpb.LeftSemiJoin,
			// Injecting nulls into the left input won't change the output.
			skipAllNullsInjection: true,
			expected:              colexectestutils.Tuples{},
		},
		{
			description: "left anti join, right non-empty",
			leftTypes:   []*types.T{types.Int},
			rightTypes:  []*types.T{types.Int},
			leftTuples:  colexectestutils.Tuples{{0}, {1}, {2}},
			rightTuples: colexectestutils.Tuples{{3}},
			leftOutCols: []uint32{0},
			joinType:    descpb.LeftAntiJoin,
			expected:    colexectestutils.Tuples{},
			// Injecting nulls into either input won't change the output.
			skipAllNullsInjection: true,
		},
		{
			description: "left anti join, left empty",
			leftTypes:   []*types.T{types.Int},
			rightTypes:  []*types.T{types.Int},
			leftTuples:  colexectestutils.Tuples{},
			rightTuples: colexectestutils.Tuples{{3}, {4}},
			leftOutCols: []uint32{0},
			joinType:    descpb.LeftAntiJoin,
			// Injecting nulls into the right input won't change the output.
			skipAllNullsInjection: true,
			expected:              colexectestutils.Tuples{},
		},
		{
			description: "left anti join, right empty",
			leftTypes:   []*types.T{types.Int},
			rightTypes:  []*types.T{types.Int},
			leftTuples:  colexectestutils.Tuples{{0}, {1}, {2}, {3}},
			rightTuples: colexectestutils.Tuples{},
			leftOutCols: []uint32{0},
			joinType:    descpb.LeftAntiJoin,
			expected:    colexectestutils.Tuples{{0}, {1}, {2}, {3}},
		},
		{
			description: "intersect all join, right smaller",
			leftTypes:   []*types.T{types.Int},
			rightTypes:  []*types.T{types.Int},
			leftTuples:  colexectestutils.Tuples{{0}, {1}, {2}, {3}, {4}},
			rightTuples: colexectestutils.Tuples{{3}, {nil}, {3}},
			leftOutCols: []uint32{0},
			joinType:    descpb.IntersectAllJoin,
			expected:    colexectestutils.Tuples{{0}, {1}, {2}},
		},
		{
			description: "intersect all join, right larger",
			leftTypes:   []*types.T{types.Int},
			rightTypes:  []*types.T{types.Int},
			leftTuples:  colexectestutils.Tuples{{0}, {1}, {2}},
			rightTuples: colexectestutils.Tuples{{3}, {nil}, {3}, {3}, {4}},
			leftOutCols: []uint32{0},
			joinType:    descpb.IntersectAllJoin,
			expected:    colexectestutils.Tuples{{0}, {1}, {2}},
		},
		{
			description: "intersect all join, left empty",
			leftTypes:   []*types.T{types.Int},
			rightTypes:  []*types.T{types.Int},
			leftTuples:  colexectestutils.Tuples{},
			rightTuples: colexectestutils.Tuples{{3}, {4}},
			leftOutCols: []uint32{0},
			joinType:    descpb.IntersectAllJoin,
			// Injecting nulls into the right input won't change the output.
			skipAllNullsInjection: true,
			expected:              colexectestutils.Tuples{},
		},
		{
			description: "intersect all join, right empty",
			leftTypes:   []*types.T{types.Int},
			rightTypes:  []*types.T{types.Int},
			leftTuples:  colexectestutils.Tuples{{0}, {1}, {2}, {3}},
			rightTuples: colexectestutils.Tuples{},
			leftOutCols: []uint32{0},
			joinType:    descpb.IntersectAllJoin,
			// Injecting nulls into the left input won't change the output.
			skipAllNullsInjection: true,
			expected:              colexectestutils.Tuples{},
		},
		{
			description: "except all join, right smaller",
			leftTypes:   []*types.T{types.Int},
			rightTypes:  []*types.T{types.Int},
			leftTuples:  colexectestutils.Tuples{{0}, {1}, {2}, {3}, {4}},
			rightTuples: colexectestutils.Tuples{{3}, {nil}, {3}},
			leftOutCols: []uint32{0},
			joinType:    descpb.ExceptAllJoin,
			expected:    colexectestutils.Tuples{{3}, {4}},
		},
		{
			description: "except all join, right larger",
			leftTypes:   []*types.T{types.Int},
			rightTypes:  []*types.T{types.Int},
			leftTuples:  colexectestutils.Tuples{{0}, {1}, {2}},
			rightTuples: colexectestutils.Tuples{{3}, {nil}, {3}, {3}, {4}},
			leftOutCols: []uint32{0},
			joinType:    descpb.ExceptAllJoin,
			// Injecting nulls into the right input won't change the output.
			skipAllNullsInjection: true,
			expected:              colexectestutils.Tuples{},
		},
		{
			description: "except all join, left empty",
			leftTypes:   []*types.T{types.Int},
			rightTypes:  []*types.T{types.Int},
			leftTuples:  colexectestutils.Tuples{},
			rightTuples: colexectestutils.Tuples{{3}, {4}},
			leftOutCols: []uint32{0},
			joinType:    descpb.ExceptAllJoin,
			// Injecting nulls into the right input won't change the output.
			skipAllNullsInjection: true,
			expected:              colexectestutils.Tuples{},
		},
		{
			description: "except all join, right empty",
			leftTypes:   []*types.T{types.Int},
			rightTypes:  []*types.T{types.Int},
			leftTuples:  colexectestutils.Tuples{{0}, {1}, {2}, {3}},
			rightTuples: colexectestutils.Tuples{},
			leftOutCols: []uint32{0},
			joinType:    descpb.ExceptAllJoin,
			expected:    colexectestutils.Tuples{{0}, {1}, {2}, {3}},
		},
	}
	for jt := range descpb.JoinType_name {
		joinType := descpb.JoinType(jt)
		tc := &joinTestCase{
			description: fmt.Sprintf("%s, both empty", joinType),
			leftTypes:   []*types.T{types.Int},
			rightTypes:  []*types.T{types.Int},
			leftTuples:  colexectestutils.Tuples{},
			rightTuples: colexectestutils.Tuples{},
			joinType:    joinType,
			expected:    colexectestutils.Tuples{},
		}
		if joinType.ShouldIncludeLeftColsInOutput() {
			tc.leftOutCols = []uint32{0}
		}
		if joinType.ShouldIncludeRightColsInOutput() {
			tc.rightOutCols = []uint32{0}
		}
		cjTestCases = append(cjTestCases, tc)
	}
	return withMirrors(cjTestCases)
}

func TestCrossJoiner(t *testing.T) {
	defer leaktest.AfterTest(t)()
	defer log.Scope(t).Close(t)

	ctx := context.Background()
	st := cluster.MakeTestingClusterSettings()
	evalCtx := eval.MakeTestingEvalContext(st)
	defer evalCtx.Stop(ctx)
	flowCtx := &execinfra.FlowCtx{
		EvalCtx: &evalCtx,
		Mon:     evalCtx.TestingMon,
		Cfg: &execinfra.ServerConfig{
			Settings: st,
		},
		DiskMonitor: testDiskMonitor,
	}
	queueCfg, cleanup := colcontainerutils.NewTestingDiskQueueCfg(t, true /* inMem */)
	defer cleanup()
	var monitorRegistry colexecargs.MonitorRegistry
	defer monitorRegistry.Close(ctx)
	var closerRegistry colexecargs.CloserRegistry
	defer closerRegistry.Close(ctx)

	for _, spillForced := range []bool{false, true} {
		flowCtx.Cfg.TestingKnobs.ForceDiskSpill = spillForced
		for _, tc := range getCJTestCases() {
			for _, tc := range tc.mutateTypes() {
				log.Infof(ctx, "spillForced=%t", spillForced)
				runHashJoinTestCase(t, tc, nil /* rng */, func(sources []colexecop.Operator) (colexecop.Operator, error) {
					spec := createSpecForHashJoiner(tc)
					args := &colexecargs.NewColOperatorArgs{
						Spec:            spec,
						Inputs:          colexectestutils.MakeInputs(sources),
						DiskQueueCfg:    queueCfg,
						FDSemaphore:     colexecop.NewTestingSemaphore(colexecop.ExternalHJMinPartitions),
						MonitorRegistry: &monitorRegistry,
						CloserRegistry:  &closerRegistry,
					}
					result, err := colexecargs.TestNewColOperator(ctx, flowCtx, args)
					return result.Root, err
				})
			}
		}
	}
}

func BenchmarkCrossJoiner(b *testing.B) {
	defer log.Scope(b).Close(b)
	ctx := context.Background()
	st := cluster.MakeTestingClusterSettings()
	evalCtx := eval.MakeTestingEvalContext(st)
	defer evalCtx.Stop(ctx)
	flowCtx := &execinfra.FlowCtx{
		EvalCtx: &evalCtx,
		Mon:     evalCtx.TestingMon,
		Cfg: &execinfra.ServerConfig{
			Settings: st,
		},
		DiskMonitor: testDiskMonitor,
	}
	nCols := 4
	sourceTypes := make([]*types.T, nCols)
	for colIdx := 0; colIdx < nCols; colIdx++ {
		sourceTypes[colIdx] = types.Int
	}

	queueCfg, cleanup := colcontainerutils.NewTestingDiskQueueCfg(b, false /* inMem */)
	defer cleanup()
	var monitorRegistry colexecargs.MonitorRegistry
	var closerRegistry colexecargs.CloserRegistry
	afterEachRun := func() {
		closerRegistry.BenchmarkReset(ctx)
		monitorRegistry.BenchmarkReset(ctx)
	}

	for _, spillForced := range []bool{false, true} {
		flowCtx.Cfg.TestingKnobs.ForceDiskSpill = spillForced
		for _, joinType := range []descpb.JoinType{descpb.InnerJoin, descpb.LeftSemiJoin} {
			for _, nRows := range []int{1, 1 << 4, 1 << 8, 1 << 11, 1 << 13} {
				cols := newIntColumns(nCols, nRows, 1 /* dupCount */)
				tc := &joinTestCase{
					joinType:   joinType,
					leftTypes:  sourceTypes,
					rightTypes: sourceTypes,
				}
				if joinType.ShouldIncludeLeftColsInOutput() {
					tc.leftOutCols = []uint32{0, 1}
				}
				if joinType.ShouldIncludeRightColsInOutput() {
					tc.rightOutCols = []uint32{2, 3}
				}
				tc.init()
				spec := createSpecForHashJoiner(tc)
				args := &colexecargs.NewColOperatorArgs{
					Spec: spec,
					// Inputs will be set below.
					Inputs:          []colexecargs.OpWithMetaInfo{{}, {}},
					DiskQueueCfg:    queueCfg,
					FDSemaphore:     colexecop.NewTestingSemaphore(VecMaxOpenFDsLimit),
					MonitorRegistry: &monitorRegistry,
					CloserRegistry:  &closerRegistry,
				}
				b.Run(fmt.Sprintf("spillForced=%t/type=%s/rows=%d", spillForced, joinType, nRows), func(b *testing.B) {
					var nOutputRows int
					if joinType == descpb.InnerJoin {
						nOutputRows = nRows * nRows
					} else {
						nOutputRows = nRows
					}
					b.SetBytes(int64(8 * nOutputRows * (len(tc.leftOutCols) + len(tc.rightOutCols))))
					b.ResetTimer()
					for i := 0; i < b.N; i++ {
						args.Inputs[0].Root = colexectestutils.NewChunkingBatchSource(testAllocator, sourceTypes, cols, nRows)
						args.Inputs[1].Root = colexectestutils.NewChunkingBatchSource(testAllocator, sourceTypes, cols, nRows)
						result, err := colexecargs.TestNewColOperator(ctx, flowCtx, args)
						require.NoError(b, err)
						cj := result.Root
						cj.Init(ctx)
						for b := cj.Next(); b.Length() > 0; b = cj.Next() {
						}
						afterEachRun()
					}
				})
			}
		}
	}
}
