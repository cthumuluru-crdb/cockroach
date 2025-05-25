package serverpb

import (
	context "context"
	"io"

	roachpb "github.com/cockroachdb/cockroach/pkg/roachpb"
	"google.golang.org/grpc"
	"storj.io/drpc"
)

type rpcStatusClient interface {
	SpanStats(ctx context.Context, in *roachpb.SpanStatsRequest) (*roachpb.SpanStatsResponse, error)
}

func NewRPCStatusClient(cc io.Closer) rpcStatusClient {
	switch cc.(type) {
	case *grpc.ClientConn:
		gc := cc.(*grpc.ClientConn)
		return (*grpcStatusClient)(&statusClient{cc: gc})
	case drpc.Conn:
		dc := cc.(drpc.Conn)
		return NewDRPCStatusClient(dc)
	default:
		// TODO(chandrat) this should never happen
		panic("unsupported connection type for rpcStatusClient")
	}
}
