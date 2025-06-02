package sidetransport

import (
	"context"

	"github.com/cockroachdb/cockroach/pkg/kv/kvserver/closedts/ctpb"
	"github.com/cockroachdb/cockroach/pkg/roachpb"
	"github.com/cockroachdb/cockroach/pkg/rpc"
	"github.com/cockroachdb/cockroach/pkg/rpc/nodedialer"
)

type nodeClientDialer nodedialer.Dialer

func (d *nodeClientDialer) DialSideTransportClient(
	ctx context.Context, nodeID roachpb.NodeID, class rpc.ConnectionClass,
) (ctpb.SideTransportClient, error) {
	nd := (*nodedialer.Dialer)(d)
	conn, err := nd.Dial(ctx, nodeID, class)
	if err != nil {
		return nil, err
	}

	return ctpb.NewSideTransportClient(conn), nil
}

func AsClientDialer(d nodeDialer) *nodeClientDialer {
	// TODO(server) can we do better?
	nd, _ := d.(*nodedialer.Dialer)
	return (*nodeClientDialer)(nd)
}
