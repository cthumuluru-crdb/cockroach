package server

import (
	"context"

	"github.com/cockroachdb/cockroach/pkg/keyvisualizer/keyvispb"
	"github.com/cockroachdb/cockroach/pkg/kv/kvserver"
	"github.com/cockroachdb/cockroach/pkg/roachpb"
	"github.com/cockroachdb/cockroach/pkg/rpc"
	"github.com/cockroachdb/cockroach/pkg/rpc/nodedialer"
)

type nodeClientDialer nodedialer.Dialer

func (d *nodeClientDialer) DialKeyVisualizerClient(
	ctx context.Context, nodeID roachpb.NodeID, class rpc.ConnectionClass,
) (keyvispb.KeyVisualizerClient, error) {
	nd := (*nodedialer.Dialer)(d)
	conn, err := nd.Dial(ctx, nodeID, class)
	if err != nil {
		return nil, err
	}

	return keyvispb.NewKeyVisualizerClient(conn), nil
}

func (d *nodeClientDialer) DialMultiRaftClient(
	ctx context.Context, nodeID roachpb.NodeID, class rpc.ConnectionClass,
) (kvserver.MultiRaftClient, error) {
	nd := (*nodedialer.Dialer)(d)
	conn, err := nd.Dial(ctx, nodeID, class)
	if err != nil {
		return nil, err
	}

	return kvserver.NewMultiRaftClient(conn), nil
}

func AsClientDialer(d *nodedialer.Dialer) *nodeClientDialer {
	return (*nodeClientDialer)(d)
}
