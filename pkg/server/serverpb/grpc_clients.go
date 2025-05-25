package serverpb

import (
	context "context"

	roachpb "github.com/cockroachdb/cockroach/pkg/roachpb"
)

type grpcStatusClient statusClient

func (g *grpcStatusClient) SpanStats(ctx context.Context, in *roachpb.SpanStatsRequest) (*roachpb.SpanStatsResponse, error) {
	return (*statusClient)(g).SpanStats(ctx, in)
}
