package upgradecluster

import (
	"context"

	"github.com/cockroachdb/cockroach/pkg/roachpb"
	"github.com/cockroachdb/cockroach/pkg/rpc"
	"github.com/cockroachdb/cockroach/pkg/rpc/nodedialer"
	"github.com/cockroachdb/cockroach/pkg/server/serverpb"
)

type nodeClientDialer nodedialer.Dialer

func (d *nodeClientDialer) DialMigrationClient(
	ctx context.Context, nodeID roachpb.NodeID, class rpc.ConnectionClass,
) (serverpb.RPCMigrationClient, error) {
	nd := (*nodedialer.Dialer)(d)
	conn, err := nd.Dial(ctx, nodeID, class)
	if err != nil {
		return nil, err
	}

	return serverpb.NewGRPCMigrationClientAdapter(conn), nil
}

func AsClientDialer(d NodeDialer) *nodeClientDialer {
	// TODO(server) can we do better?
	nd, _ := d.(*nodedialer.Dialer)
	return (*nodeClientDialer)(nd)
}
