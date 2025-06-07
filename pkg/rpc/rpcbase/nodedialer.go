// Copyright 2025 The Cockroach Authors.
//
// Use of this software is governed by the CockroachDB Software License
// included in the /LICENSE file.

package rpcbase

import (
	"context"

	"github.com/cockroachdb/cockroach/pkg/roachpb"
	"google.golang.org/grpc"
)

// TODODRPC is a marker to identify each RPC client creation site that needs to
// be updated to support DRPC.
const TODODRPC = false

// TODO(server): add comments
type dialOptions struct {
	NodeLocality roachpb.Locality
	UseBreaker   bool
}

// TODO(server): add comments
type DialOption func(*dialOptions)

func WithNodeLocality(nodeLocality roachpb.Locality) DialOption {
	return func(opts *dialOptions) {
		opts.NodeLocality = nodeLocality
	}
}

// WithNoBreaker skips the circuit breaker check before trying to connect.
// This function should only be used when there is good reason to believe
// that the node is reachable.
func WithNoBreaker() DialOption {
	return func(opts *dialOptions) {
		opts.UseBreaker = false
	}
}

func NewDefaultDialOptions() *dialOptions {
	return &dialOptions{
		NodeLocality: roachpb.Locality{},
		UseBreaker:   true,
	}
}

// NodeDialer interface defines methods for dialing peer nodes using their
// node IDs.
type NodeDialer interface {
	Dial(context.Context, roachpb.NodeID, ConnectionClass, ...DialOption) (_ *grpc.ClientConn, err error)
}
