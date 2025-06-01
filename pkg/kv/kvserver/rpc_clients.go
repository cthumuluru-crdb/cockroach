package kvserver

import (
	"context"

	"github.com/cockroachdb/cockroach/pkg/roachpb"
	"github.com/cockroachdb/cockroach/pkg/rpc"
	"github.com/cockroachdb/cockroach/pkg/rpc/nodedialer"
)

type nodeClientDialer nodedialer.Dialer

func (d *nodeClientDialer) DialMultiRaftClient(
	ctx context.Context, nodeID roachpb.NodeID, class rpc.ConnectionClass,
) (RPCMultiRaftClient, error) {
	nd := (*nodedialer.Dialer)(d)
	conn, err := nd.Dial(ctx, nodeID, class)
	if err != nil {
		return nil, err
	}

	return NewGRPCMultiRaftClientAdapter(conn), nil
}

func (d *nodeClientDialer) DialPerStoreClient(
	ctx context.Context, nodeID roachpb.NodeID, class rpc.ConnectionClass,
) (RPCPerStoreClient, error) {
	nd := (*nodedialer.Dialer)(d)
	conn, err := nd.Dial(ctx, nodeID, class)
	if err != nil {
		return nil, err
	}

	return NewGRPCPerStoreClientAdapter(conn), nil
}

func (d *nodeClientDialer) DialPerReplicaClient(
	ctx context.Context, nodeID roachpb.NodeID, class rpc.ConnectionClass,
) (RPCPerReplicaClient, error) {
	nd := (*nodedialer.Dialer)(d)
	conn, err := nd.Dial(ctx, nodeID, class)
	if err != nil {
		return nil, err
	}

	return NewGRPCPerReplicaClientAdapter(conn), nil
}

func AsClientDialer(d *nodedialer.Dialer) *nodeClientDialer {
	return (*nodeClientDialer)(d)
}
