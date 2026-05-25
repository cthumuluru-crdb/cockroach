// Copyright 2026 The Cockroach Authors.
//
// Use of this software is governed by the CockroachDB Software License
// included in the /LICENSE file.

package evalcatalog

import (
	"context"
	"fmt"
	"time"

	"github.com/cockroachdb/cockroach/pkg/sql/catalog"
	"github.com/cockroachdb/cockroach/pkg/sql/catalog/descpb"
	"github.com/cockroachdb/cockroach/pkg/sql/sem/idxtype"
	"github.com/cockroachdb/cockroach/pkg/sql/vecindex/cspann"
	"github.com/cockroachdb/cockroach/pkg/sql/vecindex/cspann/quantize"
	"github.com/cockroachdb/cockroach/pkg/sql/vecindex/vecencoding"
	"github.com/cockroachdb/cockroach/pkg/sql/vecindex/vecpb"
	"github.com/cockroachdb/cockroach/pkg/util/json"
	"github.com/cockroachdb/cockroach/pkg/util/vector"
	"github.com/cockroachdb/errors"
)

// DecodeVectorIndexKey is part of eval.CatalogBuiltins.
// It decodes a full raw key (including /Table/N/Index/N prefix) for a vector
// index and returns structured partition info as JSON.
func (ec *Builtins) DecodeVectorIndexKey(ctx context.Context, key []byte) (json.JSON, error) {
	remaining, tableID, indexID, err := ec.codec.DecodeIndexPrefix(key)
	if err != nil {
		return nil, err
	}

	tableDesc, index, err := ec.lookupVectorIndex(ctx, tableID, indexID)
	if err != nil {
		return nil, err
	}

	idxDesc := index.IndexDesc()
	numPrefixCols := int(idxDesc.Partitioning.NumImplicitColumns)
	vecKey, err := vecencoding.DecodeVectorKey(remaining, numPrefixCols)
	if err != nil {
		return nil, err
	}

	isMetadata := vecKey.Level == cspann.InvalidLevel
	builder := json.NewObjectBuilder(9)
	builder.Add("table_id", json.FromInt64(int64(tableID)))
	builder.Add("index_id", json.FromInt64(int64(indexID)))
	builder.Add("table_name", json.FromString(tableDesc.GetName()))
	builder.Add("index_name", json.FromString(index.GetName()))
	builder.Add("partition_key", json.FromInt64(int64(vecKey.PartitionKey)))
	builder.Add("level", json.FromInt64(int64(vecKey.Level)))
	builder.Add("is_metadata", json.FromBool(isMetadata))

	if !isMetadata && len(vecKey.Suffix) > 0 {
		childKey, err := vecencoding.DecodeChildKey(vecKey.Suffix, vecKey.Level)
		if err != nil {
			return nil, err
		}
		if childKey.IsPrimaryIndexBytes() {
			builder.Add("primary_key_bytes", json.FromString(fmt.Sprintf("%x", childKey.KeyBytes)))
		} else {
			builder.Add("child_partition_key", json.FromInt64(int64(childKey.PartitionKey)))
		}
	}

	return builder.Build(), nil
}

// DecodeVectorIndexValue is part of eval.CatalogBuiltins.
// It decodes a raw vector index value using the key to determine the value
// type (metadata, unquantized root vector, or RaBitQ vector).
func (ec *Builtins) DecodeVectorIndexValue(
	ctx context.Context, key, value []byte,
) (json.JSON, error) {
	remaining, tableID, indexID, err := ec.codec.DecodeIndexPrefix(key)
	if err != nil {
		return nil, err
	}

	_, index, err := ec.lookupVectorIndex(ctx, tableID, indexID)
	if err != nil {
		return nil, err
	}

	idxDesc := index.IndexDesc()
	numPrefixCols := int(idxDesc.Partitioning.NumImplicitColumns)
	vecKey, err := vecencoding.DecodeVectorKey(remaining, numPrefixCols)
	if err != nil {
		return nil, err
	}

	if vecKey.Level == cspann.InvalidLevel {
		return decodeMetadataValueJSON(value)
	}
	if vecKey.PartitionKey == cspann.RootKey {
		return decodeUnquantizedVectorJSON(value)
	}
	return decodeRaBitQVectorJSON(value, int(idxDesc.VecConfig.Dims), idxDesc.VecConfig.DistanceMetric)
}

// lookupVectorIndex looks up the table and index descriptors and validates
// that the index is a vector index.
func (ec *Builtins) lookupVectorIndex(
	ctx context.Context, tableID, indexID uint32,
) (catalog.TableDescriptor, catalog.Index, error) {
	tableDesc, err := ec.dc.ByIDWithoutLeased(ec.txn).WithoutNonPublic().MaybeGet().Table(
		ctx, descpb.ID(tableID),
	)
	if err != nil {
		return nil, nil, err
	}
	if tableDesc == nil {
		return nil, nil, errors.Newf("table %d not found", tableID)
	}

	index := catalog.FindIndexByID(tableDesc, descpb.IndexID(indexID))
	if index == nil {
		return nil, nil, errors.Newf("index %d not found in table %d", indexID, tableID)
	}
	if index.GetType() != idxtype.VECTOR {
		return nil, nil, errors.Newf(
			"index %q (id=%d) is not a vector index", index.GetName(), indexID,
		)
	}
	return tableDesc, index, nil
}

// decodeMetadataValueJSON decodes a partition metadata value into JSON.
func decodeMetadataValueJSON(value []byte) (json.JSON, error) {
	md, err := vecencoding.DecodeMetadataValue(value)
	if err != nil {
		return nil, err
	}

	centroidBuilder := json.NewArrayBuilder(len(md.Centroid))
	for _, f := range md.Centroid {
		fj, err := json.FromFloat64(float64(f))
		if err != nil {
			return nil, err
		}
		centroidBuilder.Add(fj)
	}

	builder := json.NewObjectBuilder(8)
	builder.Add("value_type", json.FromString("metadata"))
	builder.Add("level", json.FromInt64(int64(md.Level)))
	builder.Add("state", json.FromString(md.StateDetails.State.String()))
	builder.Add("target1", json.FromInt64(int64(md.StateDetails.Target1)))
	builder.Add("target2", json.FromInt64(int64(md.StateDetails.Target2)))
	builder.Add("source", json.FromInt64(int64(md.StateDetails.Source)))
	builder.Add("timestamp", json.FromString(md.StateDetails.Timestamp.UTC().Format(time.RFC3339Nano)))
	builder.Add("centroid", centroidBuilder.Build())
	return builder.Build(), nil
}

// decodeUnquantizedVectorJSON decodes an unquantized (root partition) vector
// value into JSON. The encoding prepends a 4-byte legacy field that is skipped.
func decodeUnquantizedVectorJSON(value []byte) (json.JSON, error) {
	if len(value) < 4 {
		return nil, errors.New("unquantized vector value too short")
	}
	// Skip 4-byte legacy centroid distance encoded in a previous version.
	_, v, err := vector.Decode(value[4:])
	if err != nil {
		return nil, err
	}

	arrBuilder := json.NewArrayBuilder(len(v))
	for _, f := range v {
		fj, err := json.FromFloat64(float64(f))
		if err != nil {
			return nil, err
		}
		arrBuilder.Add(fj)
	}

	builder := json.NewObjectBuilder(2)
	builder.Add("value_type", json.FromString("vector_unquantized"))
	builder.Add("vector", arrBuilder.Build())
	return builder.Build(), nil
}

// decodeRaBitQVectorJSON decodes a RaBitQ-quantized vector value into JSON.
func decodeRaBitQVectorJSON(
	value []byte, dims int, metric vecpb.DistanceMetric,
) (json.JSON, error) {
	vs := quantize.RaBitQuantizedVectorSet{
		Metric: metric,
		Codes:  quantize.MakeRaBitQCodeSet(dims),
	}
	if _, err := vecencoding.DecodeRaBitQVectorToSet(value, &vs); err != nil {
		return nil, err
	}

	codesBuilder := json.NewArrayBuilder(len(vs.Codes.Data))
	for _, c := range vs.Codes.Data {
		codesBuilder.Add(json.FromInt64(int64(c)))
	}

	numFields := 4
	if metric != vecpb.L2SquaredDistance {
		numFields = 5
	}
	builder := json.NewObjectBuilder(numFields + 1)
	builder.Add("value_type", json.FromString("vector_rabitq"))
	builder.Add("code_count", json.FromInt64(int64(vs.CodeCounts[0])))

	centroidDist, err := json.FromFloat64(float64(vs.CentroidDistances[0]))
	if err != nil {
		return nil, err
	}
	builder.Add("centroid_distance", centroidDist)

	qdp, err := json.FromFloat64(float64(vs.QuantizedDotProducts[0]))
	if err != nil {
		return nil, err
	}
	builder.Add("quantized_dot_product", qdp)

	if metric != vecpb.L2SquaredDistance {
		cdp, err := json.FromFloat64(float64(vs.CentroidDotProducts[0]))
		if err != nil {
			return nil, err
		}
		builder.Add("centroid_dot_product", cdp)
	}
	builder.Add("codes", codesBuilder.Build())
	return builder.Build(), nil
}
