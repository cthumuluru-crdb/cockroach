package sql

import (
	"context"

	"github.com/cockroachdb/cockroach/pkg/roachpb"
	"github.com/cockroachdb/cockroach/pkg/rpc"
	"github.com/cockroachdb/cockroach/pkg/rpc/nodedialer"
	"github.com/cockroachdb/cockroach/pkg/sql/execinfrapb"
)

type nodeClientDialer nodedialer.Dialer

func (d *nodeClientDialer) DialDistSQLClient(
	ctx context.Context, nodeID roachpb.NodeID, class rpc.ConnectionClass,
) (execinfrapb.DistSQLClient, error) {
	nd := (*nodedialer.Dialer)(d)
	conn, err := nd.Dial(ctx, nodeID, class)
	if err != nil {
		return nil, err
	}

	return execinfrapb.NewDistSQLClient(conn), nil
}

func AsClientDialer(d *nodedialer.Dialer) *nodeClientDialer {
	return (*nodeClientDialer)(d)
}
