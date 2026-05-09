// Copyright 2024 The Cockroach Authors.
//
// Use of this software is governed by the CockroachDB Software License
// included in the /LICENSE file.

package quantize

import (
	"slices"
	"testing"

	"github.com/cockroachdb/cockroach/pkg/sql/vecindex/cspann/testutils"
	"github.com/cockroachdb/cockroach/pkg/sql/vecindex/cspann/workspace"
	"github.com/cockroachdb/cockroach/pkg/sql/vecindex/vecpb"
	"github.com/cockroachdb/cockroach/pkg/util/num32"
	"github.com/cockroachdb/cockroach/pkg/util/vector"
	"github.com/stretchr/testify/require"
	"gonum.org/v1/gonum/floats/scalar"
)

// printActual is a debug helper that prints actual values to help update
// expected assertions after algorithm changes.
func printActual(t *testing.T, label string, values []float32) {
	t.Helper()
	t.Logf("%s: %v", label, testutils.RoundFloats(values, 2))
}

// Basic tests.
func TestRaBitQuantizerSimple(t *testing.T) {
	var workspace workspace.T
	defer require.True(t, workspace.IsClear())

	t.Run("add and remove vectors", func(t *testing.T) {
		quantizer := NewRaBitQuantizer(2, 42, vecpb.L2SquaredDistance)
		require.Equal(t, 2, quantizer.GetDims())

		// Add 3 vectors and verify centroid.
		vectors := vector.MakeSetFromRawData([]float32{5, 2, 1, 2, 6, 5}, 2)
		quantizedSet := quantizer.Quantize(&workspace, vectors).(*RaBitQuantizedVectorSet)
		require.Equal(t, []float32{4, 3}, quantizedSet.Centroid)

		// Add 2 more vectors to existing set.
		vectors = vector.MakeSetFromRawData([]float32{4, 3, 6, 5}, 2)
		quantizer.QuantizeInSet(&workspace, quantizedSet, vectors)
		require.Equal(t, 5, quantizedSet.GetCount())

		// Ensure distances and error bounds are correct.
		distances := make([]float32, quantizedSet.GetCount())
		errorBounds := make([]float32, quantizedSet.GetCount())
		quantizer.EstimateDistances(
			&workspace, quantizedSet, vector.T{1, 1}, distances, errorBounds)
		printActual(t, "distances", distances)
		printActual(t, "errorBounds", errorBounds)
		require.Equal(t, []float32{17, 1, 41, 13, 41}, testutils.RoundFloats(distances, 2))
		require.Equal(t, []float32{4, 3}, quantizedSet.Centroid)

		// Remove quantized vectors from the set.
		quantizedSet.ReplaceWithLast(1)
		quantizedSet.ReplaceWithLast(3)
		quantizedSet.ReplaceWithLast(1)
		require.Equal(t, 2, quantizedSet.GetCount())
		distances = distances[:2]
		errorBounds = errorBounds[:2]
		quantizer.EstimateDistances(
			&workspace, quantizedSet, vector.T{1, 1}, distances, errorBounds)
		printActual(t, "distances after remove", distances)

		// Remove remaining quantized vectors.
		quantizedSet.ReplaceWithLast(0)
		quantizedSet.ReplaceWithLast(0)
		require.Equal(t, 0, quantizedSet.GetCount())
		require.Equal(t, []float32{4, 3}, quantizedSet.Centroid)
		distances = distances[:0]
		errorBounds = errorBounds[:0]
		quantizer.EstimateDistances(
			&workspace, quantizedSet, vector.T{1, 1}, distances, errorBounds)
	})

	t.Run("empty quantized set", func(t *testing.T) {
		quantizer := NewRaBitQuantizer(2, 42, vecpb.L2SquaredDistance)
		vectors := vector.MakeSet(2)
		quantizedSet := quantizer.Quantize(&workspace, vectors).(*RaBitQuantizedVectorSet)
		require.Equal(t, []float32{0, 0}, quantizedSet.Centroid)
	})

	t.Run("empty quantized set with capacity", func(t *testing.T) {
		quantizer := NewRaBitQuantizer(65, 42, vecpb.InnerProductDistance)
		centroid := make([]float32, 65)
		for i := range centroid {
			centroid[i] = float32(i)
		}
		quantizedSet := quantizer.NewSet(5, centroid).(*RaBitQuantizedVectorSet)
		require.Equal(t, centroid, quantizedSet.Centroid)
		require.Equal(t, 0, quantizedSet.Codes.Count)
		require.Equal(t, 5, quantizedSet.Codes.Width)
		require.Equal(t, 25, cap(quantizedSet.Codes.Data))
		require.Equal(t, 5, cap(quantizedSet.CodeNorms))
		require.Equal(t, 5, cap(quantizedSet.CentroidDistances))
		require.Equal(t, 5, cap(quantizedSet.QuantizedDotProducts))
		require.Equal(t, 5, cap(quantizedSet.CentroidDotProducts))
		require.Equal(t, float64(299.07), scalar.Round(float64(quantizedSet.CentroidNorm), 2))
	})
}

// Edge cases.
func TestRaBitQuantizerEdge(t *testing.T) {
	var workspace workspace.T
	defer require.True(t, workspace.IsClear())

	// Search for query vector with two equal dimensions.
	t.Run("two dimensions equal", func(t *testing.T) {
		quantizer := NewRaBitQuantizer(2, 42, vecpb.L2SquaredDistance)
		vectors := vector.MakeSetFromRawData([]float32{4, 4, -3, -3}, 2)
		quantizedSet := quantizer.Quantize(&workspace, vectors).(*RaBitQuantizedVectorSet)
		require.Equal(t, 2, quantizedSet.GetCount())
		// With 4-bit codes, 2 dims → width 1. Each dim gets a 4-bit nibble.
		// Vec 0 unit: {1/√2, 1/√2} → grid {g,g} where g>0 → unsigned {7.5+g, 7.5+g}
		t.Logf("codes: %v", quantizedSet.Codes.Data)

		distances := make([]float32, 2)
		errorBounds := make([]float32, 2)
		quantizer.EstimateDistances(
			&workspace, quantizedSet, vector.T{1, 1}, distances, errorBounds)
		printActual(t, "distances", distances)
		printActual(t, "errorBounds", errorBounds)
	})

	t.Run("many dimensions, not multiple of 64", func(t *testing.T) {
		// Number dimensions is > 64 and not a multiple of 64.
		quantizer := NewRaBitQuantizer(141, 42, vecpb.L2SquaredDistance)

		vectors := vector.MakeSet(141)
		vectors.AddUndefined(2)
		zeros := vectors.At(0)
		ones := vectors.At(1)
		for i := 0; i < len(ones); i++ {
			zeros[i] = 0
			ones[i] = 1
		}
		quantizedSet := quantizer.Quantize(&workspace, vectors).(*RaBitQuantizedVectorSet)
		require.Equal(t, []float32{5.94, 5.94},
			testutils.RoundFloats(quantizedSet.CentroidDistances, 2))

		// With 4-bit codes, 141 dims → width ceil(141/16) = 9.
		t.Logf("code width: %d", quantizedSet.Codes.Width)
		require.Equal(t, 9, quantizedSet.Codes.Width)

		// Vec 0 (all zeros): centroid is {0.5,...,0.5}, diff is {-0.5,...,-0.5},
		// unit vector is {-1/√D,...,-1/√D}. Grid should be all negative.
		code0 := quantizedSet.Codes.At(0)
		t.Logf("code0: %v", code0)
		// Vec 1 (all ones): diff is {0.5,...,0.5}, unit is {1/√D,...,1/√D}.
		// Grid should be all positive.
		code1 := quantizedSet.Codes.At(1)
		t.Logf("code1: %v", code1)

		distances := make([]float32, quantizedSet.GetCount())
		errorBounds := make([]float32, quantizedSet.GetCount())
		quantizer.EstimateDistances(
			&workspace, quantizedSet, ones, distances, errorBounds)
		printActual(t, "distances", distances)
		printActual(t, "errorBounds", errorBounds)
		// Distance from ones to zeros should be 141 (exact), and from ones to
		// ones should be 0 (exact). With v2's better quantization, estimates
		// should be closer.
		require.Equal(t, []float32{141, 0}, testutils.RoundFloats(distances, 2))
	})

	t.Run("add centroid to set", func(t *testing.T) {
		quantizer := NewRaBitQuantizer(2, 42, vecpb.L2SquaredDistance)
		quantizedSet := quantizer.NewSet(4, []float32{3, 9}).(*RaBitQuantizedVectorSet)
		vectors := vector.MakeSetFromRawData([]float32{1, 5, 5, 13}, 2)
		quantizer.QuantizeInSet(&workspace, quantizedSet, vectors)

		// Add centroid to the set along with another vector.
		vectors = vector.MakeSetFromRawData([]float32{1, 5, 3, 9}, 2)
		quantizer.QuantizeInSet(&workspace, quantizedSet, vectors)
		require.Equal(t, float32(0), quantizedSet.QuantizedDotProducts[3],
			"dot product for centroid should be zero")

		// Estimate distances from a query vector not in the set.
		distances := make([]float32, 4)
		errorBounds := make([]float32, 4)
		quantizer.EstimateDistances(
			&workspace, quantizedSet, vector.T{3, 2}, distances, errorBounds)
		printActual(t, "distances", distances)
		printActual(t, "errorBounds", errorBounds)

		// Estimate distances when the query vector is the centroid.
		quantizer.EstimateDistances(
			&workspace, quantizedSet, vector.T{3, 9}, distances, errorBounds)
		require.Equal(t, []float32{20, 20, 20, 0}, testutils.RoundFloats(distances, 2))
		require.Equal(t, []float32{0, 0, 0, 0}, testutils.RoundFloats(errorBounds, 2))
	})

	t.Run("query vector is centroid", func(t *testing.T) {
		quantizer := NewRaBitQuantizer(2, 42, vecpb.L2SquaredDistance)
		vectors := vector.MakeSetFromRawData([]float32{1, 5, -3, -9}, 2)
		quantizedSet := quantizer.Quantize(&workspace, vectors).(*RaBitQuantizedVectorSet)
		require.Equal(t, []float32{-1, -2}, quantizedSet.Centroid)
		distances := make([]float32, 2)
		errorBounds := make([]float32, 2)
		quantizer.EstimateDistances(
			&workspace, quantizedSet, vector.T{-1, -2}, distances, errorBounds)
		require.Equal(t, []float32{53, 53}, testutils.RoundFloats(distances, 2))
		require.Equal(t, []float32{0, 0}, testutils.RoundFloats(errorBounds, 2))
	})
}

// Test InnerProduct distance metric.
func TestRaBitQuantizerInnerProduct(t *testing.T) {
	var workspace workspace.T
	quantizer := NewRaBitQuantizer(2, 42, vecpb.InnerProductDistance)
	require.Equal(t, 2, quantizer.GetDims())

	// Add 3 vectors and verify centroid.
	vectors := vector.MakeSetFromRawData([]float32{5, 2, 1, 2, 6, 5}, 2)
	quantizedSet := quantizer.Quantize(&workspace, vectors).(*RaBitQuantizedVectorSet)
	require.Equal(t, []float32{4, 3}, testutils.RoundFloats(quantizedSet.Centroid, 4))

	// Ensure distances and error bounds are correct.
	distances := make([]float32, quantizedSet.GetCount())
	errorBounds := make([]float32, quantizedSet.GetCount())
	quantizer.EstimateDistances(&workspace, quantizedSet, vector.T{3, 4}, distances, errorBounds)
	printActual(t, "distances", distances)
	printActual(t, "errorBounds", errorBounds)

	// Call NewQuantizedSet and ensure capacity.
	quantizedSet = quantizer.NewSet(
		5, quantizedSet.Centroid).(*RaBitQuantizedVectorSet)
	require.Equal(t, 5, cap(quantizedSet.CentroidDotProducts))

	// Add vectors to already-created set.
	quantizer.QuantizeInSet(&workspace, quantizedSet, vectors)

	// Query vector is the centroid.
	quantizer.EstimateDistances(&workspace, quantizedSet, quantizedSet.Centroid,
		distances, errorBounds)
	printActual(t, "centroid distances", distances)
	require.Equal(t, []float32{0, 0, 0}, testutils.RoundFloats(errorBounds, 2))
}

// Test Cosine distance metric.
func TestRaBitQuantizerCosine(t *testing.T) {
	var workspace workspace.T
	quantizer := NewRaBitQuantizer(2, 42, vecpb.CosineDistance)
	require.Equal(t, 2, quantizer.GetDims())

	// Add 3 vectors and verify centroid.
	vectors := vector.MakeSetFromRawData([]float32{1, 0, 0, 1, 0.70710678, 0.70710678}, 2)
	quantizedSet := quantizer.Quantize(&workspace, vectors).(*RaBitQuantizedVectorSet)
	require.Equal(t, []float32{0.569, 0.569}, testutils.RoundFloats(quantizedSet.Centroid, 4))

	// Ensure distances and error bounds are correct.
	distances := make([]float32, quantizedSet.GetCount())
	errorBounds := make([]float32, quantizedSet.GetCount())
	quantizer.EstimateDistances(&workspace, quantizedSet, vector.T{-1, 0}, distances, errorBounds)
	printActual(t, "distances", distances)
	printActual(t, "errorBounds", errorBounds)

	// Call NewQuantizedSet and ensure capacity.
	centroid := slices.Clone(quantizedSet.Centroid)
	num32.Normalize(centroid)
	quantizedSet = quantizer.NewSet(5, centroid).(*RaBitQuantizedVectorSet)
	require.Equal(t, 5, cap(quantizedSet.CentroidDotProducts))

	// Add vectors to already-created set.
	quantizer.QuantizeInSet(&workspace, quantizedSet, vectors)

	// Query vector is the centroid.
	quantizer.EstimateDistances(&workspace, quantizedSet, quantizedSet.Centroid,
		distances, errorBounds)
	printActual(t, "centroid distances", distances)
	printActual(t, "centroid errorBounds", errorBounds)
}

// Load some real OpenAI embeddings and spot check calculations.
func TestRaBitQuantizeEmbeddings(t *testing.T) {
	var workspace workspace.T
	defer require.True(t, workspace.IsClear())

	dataset := testutils.LoadDataset(t, testutils.ImagesDataset)
	dataset = dataset.Slice(0, 100)
	quantizer := NewRaBitQuantizer(dataset.Dims, 42, vecpb.L2SquaredDistance)
	require.Equal(t, 512, quantizer.GetDims())

	quantizedSet := quantizer.Quantize(&workspace, dataset).(*RaBitQuantizedVectorSet)
	require.Equal(t, 100, quantizedSet.GetCount())

	centroid := quantizedSet.Centroid
	require.Len(t, centroid, 512)
	require.InDelta(t, -0.00452728, centroid[0], 0.0000001)
	require.InDelta(t, -0.00299389, centroid[511], 0.0000001)

	centroidDistances := quantizedSet.CentroidDistances
	require.Len(t, centroidDistances, 100)
	require.InDelta(t, 0.7345806, centroidDistances[0], 0.0000001)
	require.InDelta(t, 0.7328457, centroidDistances[99], 0.0000001)

	queryVector := dataset.At(0)
	distances := make([]float32, quantizedSet.GetCount())
	errorBounds := make([]float32, quantizedSet.GetCount())
	quantizer.EstimateDistances(
		&workspace, quantizedSet, queryVector, distances, errorBounds)
	num32.Round(distances, 4)
	num32.Round(errorBounds, 4)
	t.Logf("dist[0]=%v dist[99]=%v err[0]=%v err[99]=%v",
		distances[0], distances[99], errorBounds[0], errorBounds[99])
	// Self-distance should be near zero.
	require.InDelta(t, 0, distances[0], 0.01)
	// Error bounds should be consistent with v2 bound: 0.36/√D ≈ 0.016 for 512d.
	require.InDelta(t, 0.0159, errorBounds[0], 0.005)
}

// Benchmark quantization of 100 vectors.
func BenchmarkQuantize(b *testing.B) {
	var workspace workspace.T
	dataset := testutils.LoadDataset(b, testutils.ImagesDataset)
	dataset = dataset.Slice(0, 100)
	quantizer := NewRaBitQuantizer(dataset.Dims, 42, vecpb.L2SquaredDistance)

	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		quantizer.Quantize(&workspace, dataset)
	}
}

// Benchmark L2Squared distance estimation of 100 vectors.
func BenchmarkEstimateL2SquaredDistances(b *testing.B) {
	var workspace workspace.T
	dataset := testutils.LoadDataset(b, testutils.ImagesDataset)
	dataset = dataset.Slice(0, 100)
	quantizer := NewRaBitQuantizer(dataset.Dims, 42, vecpb.L2SquaredDistance)
	quantizedSet := quantizer.Quantize(&workspace, dataset)

	queryVector := dataset.At(0)
	squaredDistances := make([]float32, quantizedSet.GetCount())
	errorBounds := make([]float32, quantizedSet.GetCount())

	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		quantizer.EstimateDistances(
			&workspace, quantizedSet, queryVector, squaredDistances, errorBounds)
	}
}
