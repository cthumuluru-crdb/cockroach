package blobs

import (
	"context"

	"github.com/cockroachdb/cockroach/pkg/blobs/blobspb"
	"github.com/cockroachdb/cockroach/pkg/roachpb"
	"github.com/cockroachdb/cockroach/pkg/rpc"
	"github.com/cockroachdb/cockroach/pkg/rpc/nodedialer"
)

type nodeClientDialer nodedialer.Dialer

func (d *nodeClientDialer) DialBlobClient(
	ctx context.Context, nodeID roachpb.NodeID, class rpc.ConnectionClass,
) (blobspb.BlobClient, error) {
	nd := (*nodedialer.Dialer)(d)
	conn, err := nd.Dial(ctx, nodeID, class)
	if err != nil {
		return nil, err
	}

	return blobspb.NewBlobClient(conn), nil
}

func AsClientDialer(d *nodedialer.Dialer) *nodeClientDialer {
	return (*nodeClientDialer)(d)
}
