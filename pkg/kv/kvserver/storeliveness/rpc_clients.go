package storeliveness

import (
	"context"

	slpb "github.com/cockroachdb/cockroach/pkg/kv/kvserver/storeliveness/storelivenesspb"
	"github.com/cockroachdb/cockroach/pkg/roachpb"
	"github.com/cockroachdb/cockroach/pkg/rpc"
	"github.com/cockroachdb/cockroach/pkg/rpc/nodedialer"
)

type nodeClientDialer nodedialer.Dialer

func (d *nodeClientDialer) DialStoreLivenessClient(
	ctx context.Context, nodeID roachpb.NodeID, class rpc.ConnectionClass,
) (slpb.StoreLivenessClient, error) {
	nd := (*nodedialer.Dialer)(d)
	conn, err := nd.Dial(ctx, nodeID, class)
	if err != nil {
		return nil, err
	}

	return slpb.NewStoreLivenessClient(conn), nil
}

func AsClientDialer(d *nodedialer.Dialer) *nodeClientDialer {
	return (*nodeClientDialer)(d)
}
