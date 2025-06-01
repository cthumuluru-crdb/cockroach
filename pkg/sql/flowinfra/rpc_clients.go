package flowinfra

import (
	"context"
	"time"

	"github.com/cockroachdb/cockroach/pkg/base"
	"github.com/cockroachdb/cockroach/pkg/rpc/nodedialer"
	"github.com/cockroachdb/cockroach/pkg/sql/execinfra"
	"github.com/cockroachdb/cockroach/pkg/sql/execinfrapb"
)

type nodeClientDialer nodedialer.Dialer

func (d *nodeClientDialer) DialDistSQLClient(
	ctx context.Context, sqlInstanceID base.SQLInstanceID, timeout time.Duration,
) (execinfrapb.RPCDistSQLClient, error) {
	nd := (*nodedialer.Dialer)(d)
	conn, err := execinfra.GetConnForOutbox(ctx, nd, sqlInstanceID, timeout)
	if err != nil {
		return nil, err
	}

	return execinfrapb.NewGRPCDistSQLClientAdapter(conn), nil
}

func AsClientDialer(d execinfra.Dialer) *nodeClientDialer {
	nd, _ := d.(*nodedialer.Dialer)
	return (*nodeClientDialer)(nd)
}
