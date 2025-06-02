package collector

import (
	"context"

	"github.com/cockroachdb/cockroach/pkg/roachpb"
	"github.com/cockroachdb/cockroach/pkg/rpc"
	"github.com/cockroachdb/cockroach/pkg/rpc/nodedialer"
	"github.com/cockroachdb/cockroach/pkg/util/tracing/tracingservicepb"
)

type nodeClientDialer nodedialer.Dialer

func (d *nodeClientDialer) DialMigrationClient(
	ctx context.Context, nodeID roachpb.NodeID, class rpc.ConnectionClass,
) (tracingservicepb.TracingClient, error) {
	nd := (*nodedialer.Dialer)(d)
	conn, err := nd.Dial(ctx, nodeID, class)
	if err != nil {
		return nil, err
	}

	return tracingservicepb.NewTracingClient(conn), nil
}

func AsClientDialer(d *nodedialer.Dialer) *nodeClientDialer {
	return (*nodeClientDialer)(d)
}
