// Copyright 2024 The Cockroach Authors.
//
// Use of this software is governed by the CockroachDB Software License
// included in the /LICENSE file.

package quantize

import (
	"math"
	"slices"

	"github.com/cockroachdb/cockroach/pkg/sql/vecindex/cspann/utils"
	"github.com/cockroachdb/cockroach/pkg/sql/vecindex/cspann/workspace"
	"github.com/cockroachdb/cockroach/pkg/sql/vecindex/vecpb"
	"github.com/cockroachdb/cockroach/pkg/util/buildutil"
	"github.com/cockroachdb/cockroach/pkg/util/num32"
	"github.com/cockroachdb/cockroach/pkg/util/vector"
	"github.com/cockroachdb/errors"
)

// RaBitQuantizer quantizes vectors according to the RaBitQ v2 algorithm:
//
//	"Practical and Asymptotically Optimal Quantization of High-Dimensional
//	Vectors in Euclidean Space for Approximate Nearest Neighbor Search"
//	by Jianyang Gao & Cheng Long.
//
// This implementation uses B=4 bits per dimension, mapping each dimension to
// one of 16 grid values {-7.5, -6.5, ..., 7.5}. The grid vector is chosen
// to maximize cosine similarity with the centroid-relative unit vector, using
// the min-heap sweep of critical rescaling factors (Algorithm 1 in the paper).
//
// Error bound: ε < 2^(-B) * 5.75 / √D ≈ 0.36/√D with >99.9% probability,
// compared to 1/√D for v1. This gives ~3.6x tighter error bounds.
//
// All methods in RaBitQuantizer are thread-safe. It is intended to be cached
// on a per-process basis and reused across all threads that query the same
// vector index. This is important, because the ROT matrix is expensive to
// generate and can use quite a bit of memory.
type RaBitQuantizer struct {
	// dims is the dimensionality of vectors that can be quantized.
	dims int
	// sqrtDims is the precomputed square root of the "dims" field.
	sqrtDims float32
	// sqrtDimsInv precomputes "1 / sqrtDims".
	sqrtDimsInv float32
	// distanceMetric determines which distance function to use.
	distanceMetric vecpb.DistanceMetric
}

// raBitQuantizedVector adds extra storage space for the special case where the
// vector set has at most one vector. In that case, the vector set slices point
// to the statically-allocated arrays in this struct.
type raBitQuantizedVector struct {
	RaBitQuantizedVectorSet
	codeNormStorage            [1]float32
	centroidDistanceStorage    [1]float32
	quantizedDotProductStorage [1]float32
	centroidDotProductStorage  [1]float32
}

var _ Quantizer = (*RaBitQuantizer)(nil)

// NewRaBitQuantizer returns a new RaBitQ quantizer that quantizes vectors with
// the given number of dimensions. The provided seed is retained for interface
// stability but is unused in v2 (v1 used it for query-side random offsets).
func NewRaBitQuantizer(dims int, seed int64, distanceMetric vecpb.DistanceMetric) Quantizer {
	if dims <= 0 {
		panic(errors.AssertionFailedf("dimensions are not positive: %d", dims))
	}

	sqrtDims := num32.Sqrt(float32(dims))
	return &RaBitQuantizer{
		dims:           dims,
		sqrtDims:       sqrtDims,
		sqrtDimsInv:    1.0 / sqrtDims,
		distanceMetric: distanceMetric,
	}
}

// GetDims implements the Quantizer interface.
func (q *RaBitQuantizer) GetDims() int {
	return q.dims
}

// GetDistanceMetric implements the Quantizer interface.
func (q *RaBitQuantizer) GetDistanceMetric() vecpb.DistanceMetric {
	return q.distanceMetric
}

// Quantize implements the Quantizer interface.
func (q *RaBitQuantizer) Quantize(w *workspace.T, vectors vector.Set) QuantizedVectorSet {
	var centroid vector.T
	if vectors.Count == 1 {
		// If quantizing a single vector, it is the centroid of the set.
		centroid = vectors.At(0)
	} else {
		// Compute the centroid.
		centroid = vectors.Centroid(make(vector.T, vectors.Dims))
	}

	quantizedSet := q.NewSet(vectors.Count, centroid)
	q.quantizeHelper(w, quantizedSet.(*RaBitQuantizedVectorSet), vectors)
	return quantizedSet
}

// QuantizeInSet implements the Quantizer interface.
func (q *RaBitQuantizer) QuantizeInSet(
	w *workspace.T, quantizedSet QuantizedVectorSet, vectors vector.Set,
) {
	q.quantizeHelper(w, quantizedSet.(*RaBitQuantizedVectorSet), vectors)
}

// NewSet implements the Quantizer interface
func (q *RaBitQuantizer) NewSet(capacity int, centroid vector.T) QuantizedVectorSet {
	var vs *RaBitQuantizedVectorSet

	if capacity <= 1 {
		// Special case capacity of zero or one by using in-line storage.
		quantized := &raBitQuantizedVector{}
		quantized.CodeNorms = quantized.codeNormStorage[:0]
		quantized.CentroidDistances = quantized.centroidDistanceStorage[:0]
		quantized.QuantizedDotProducts = quantized.quantizedDotProductStorage[:0]

		// L2Squared doesn't use this.
		if q.distanceMetric != vecpb.L2SquaredDistance {
			quantized.CentroidDotProducts = quantized.centroidDotProductStorage[:0]
		}
		vs = &quantized.RaBitQuantizedVectorSet
	} else {
		vs = &RaBitQuantizedVectorSet{
			CodeNorms:            make([]float32, 0, capacity),
			CentroidDistances:    make([]float32, 0, capacity),
			QuantizedDotProducts: make([]float32, 0, capacity),
		}
		// L2Squared doesn't use these, so don't make extra allocation or calculation.
		if q.distanceMetric != vecpb.L2SquaredDistance {
			vs.CentroidDotProducts = make([]float32, 0, capacity)
		}
	}

	vs.Metric = q.distanceMetric
	vs.Centroid = centroid
	codeWidth := RaBitQCodeSetWidth(q.GetDims())
	dataBuffer := make([]uint64, 0, capacity*codeWidth)
	vs.Codes = MakeRaBitQCodeSetFromRawData(dataBuffer, codeWidth)
	if q.distanceMetric != vecpb.L2SquaredDistance {
		vs.CentroidNorm = num32.Norm(centroid)
	}

	return vs
}

// EstimateDistances implements the Quantizer interface.
//
// For each query, we compute the dot product between the unsigned 4-bit data
// codes and the float query unit vector directly (no query quantization).
// The estimator is:
//
//	<ō,q'> = (1/||ȳ||) * (<ȳ_u, q'> - 7.5 * Σq'[i])
//	<o,q> ≈ <ō,q'> / <ō,o>
//
// where ȳ_u[i] = ȳ[i] + 7.5 is the unsigned form stored in the codes, and
// 7.5 = 2^(B-1) - 0.5 is the unsigned offset for B=4.
func (q *RaBitQuantizer) EstimateDistances(
	w *workspace.T,
	quantizedSet QuantizedVectorSet,
	queryVector vector.T,
	distances []float32,
	errorBounds []float32,
) {
	if buildutil.CrdbTestBuild && q.distanceMetric == vecpb.CosineDistance {
		utils.ValidateUnitVector(queryVector)
	}

	raBitSet := quantizedSet.(*RaBitQuantizedVectorSet)

	// Allocate temp space for calculations.
	tempVectors := w.AllocVectorSet(1, q.dims)
	defer w.FreeVectorSet(tempVectors)

	// Normalize the query vector to a unit vector, with respect to the centroid.
	tempQueryDiff := tempVectors.At(0)
	num32.SubTo(tempQueryDiff, queryVector, raBitSet.Centroid)
	queryCentroidDistance := num32.Norm(tempQueryDiff)

	if queryCentroidDistance == 0 {
		q.GetCentroidDistances(quantizedSet, distances, false /* spherical */)
		num32.Zero(errorBounds)
		return
	}

	var squaredCentroidNorm, queryCentroidDotProduct float32
	if q.distanceMetric != vecpb.L2SquaredDistance {
		queryCentroidDotProduct = num32.Dot(queryVector, raBitSet.Centroid)
		squaredCentroidNorm = raBitSet.CentroidNorm * raBitSet.CentroidNorm
	}

	tempQueryUnitVector := tempQueryDiff
	num32.Scale(1.0/queryCentroidDistance, tempQueryUnitVector)

	// Precompute sumQ = Σq'[i] for the unsigned-to-signed offset correction.
	var sumQ float32
	for _, v := range tempQueryUnitVector {
		sumQ += v
	}

	// Error bound for v2 with B=4: ε < 2^(-4) * 5.75 / √D ≈ 0.36/√D.
	const errorBoundFactor = 0.359375 // 5.75 / 16

	count := raBitSet.GetCount()
	for i := range count {
		code := raBitSet.Codes.At(i)

		// Compute <ȳ_u, q'> by extracting 4-bit nibbles from the packed code.
		// Nibbles are stored big-endian: the first dimension occupies the
		// most-significant nibble of the first uint64.
		var dotYuQ float32
		dim := 0
		for _, word := range code {
			for nibbleIdx := 60; nibbleIdx >= 0 && dim < q.dims; nibbleIdx -= 4 {
				yu := float32((word >> uint(nibbleIdx)) & 0xF)
				dotYuQ += yu * tempQueryUnitVector[dim]
				dim++
			}
		}

		// Compute the inner product estimator.
		//   <ō,q'> = (1/||ȳ||) * (<ȳ_u, q'> - 7.5 * sumQ)
		//   <o,q> ≈ <ō,q'> * (1/<ō,o>)    [QuantizedDotProducts stores 1/<ō,o>]
		codeNorm := raBitSet.CodeNorms[i]
		var estimator float32
		if codeNorm != 0 {
			estimator = (dotYuQ - 7.5*sumQ) / codeNorm * raBitSet.QuantizedDotProducts[i]
		}

		dataCentroidDistance := raBitSet.CentroidDistances[i]

		switch q.distanceMetric {
		case vecpb.L2SquaredDistance:
			distance := dataCentroidDistance * dataCentroidDistance
			distance += queryCentroidDistance * queryCentroidDistance
			multiplier := 2 * dataCentroidDistance * queryCentroidDistance
			distance -= multiplier * estimator

			errorBound := multiplier * errorBoundFactor * q.sqrtDimsInv
			if distance < 0 {
				errorBound = max(errorBound+distance, 0)
				distance = 0
			}

			distances[i] = distance
			errorBounds[i] = errorBound

		case vecpb.InnerProductDistance, vecpb.CosineDistance:
			multiplier := dataCentroidDistance * queryCentroidDistance
			innerProduct := multiplier*estimator +
				raBitSet.CentroidDotProducts[i] + queryCentroidDotProduct - squaredCentroidNorm

			errorBound := multiplier * errorBoundFactor * q.sqrtDimsInv

			var distance float32
			if q.distanceMetric == vecpb.InnerProductDistance {
				distance = -innerProduct
			} else {
				distance = 1 - innerProduct
				if distance < 0 {
					errorBound = max(errorBound+distance, 0)
					distance = 0
				} else if distance > 2 {
					errorBound = max(min(errorBound-(distance-2), 2), 0)
					distance = 2
				}
			}

			distances[i] = distance
			errorBounds[i] = errorBound

		default:
			panic(errors.AssertionFailedf(
				"RaBitQuantizer does not support distance metric %s", q.distanceMetric))
		}
	}
}

// GetCentroidDistances implements the Quantizer interface.
func (q *RaBitQuantizer) GetCentroidDistances(
	quantizedSet QuantizedVectorSet, distances []float32, spherical bool,
) {
	raBitSet := quantizedSet.(*RaBitQuantizedVectorSet)

	switch q.distanceMetric {
	case vecpb.L2SquaredDistance:
		// The distance from the query to the data vectors are just the centroid
		// distances that have already been calculated, but just need to be
		// squared.
		num32.MulTo(distances, raBitSet.CentroidDistances, raBitSet.CentroidDistances)

	case vecpb.InnerProductDistance:
		// Need to negate precomputed centroid dot products to compute inner
		// product distance.
		multiplier := float32(-1)
		if spherical && raBitSet.CentroidNorm != 0 {
			// Convert the mean centroid dot product into a spherical centroid
			// dot product by dividing by the centroid's norm.
			multiplier /= raBitSet.CentroidNorm
		}
		num32.ScaleTo(distances, multiplier, raBitSet.CentroidDotProducts)

	case vecpb.CosineDistance:
		// Cosine distance = 1 - dot product when vectors are normalized. The
		// precomputed centroid dot products were computed with normalized data
		// vectors, but the centroid was not normalized. Do that now by dividing
		// the dot products by the centroid's norm. Also negate the result.
		multiplier := float32(-1)
		if raBitSet.CentroidNorm != 0 {
			multiplier /= raBitSet.CentroidNorm
		}
		num32.ScaleTo(distances, multiplier, raBitSet.CentroidDotProducts)
		num32.AddConst(1, distances)

	default:
		panic(errors.AssertionFailedf(
			"RaBitQuantizer does not support distance metric %s", q.distanceMetric))
	}
}

// critFactor stores a critical rescaling factor and the dimension it affects.
// Used by quantizeHelper's Algorithm 1 sweep.
type critFactor struct {
	t      float64
	dim    int
	newVal float32 // the grid value this dimension would take at this factor
}

// quantizeHelper quantizes the given set of vectors and adds the quantization
// information to the provided quantized vector set.
//
// For each data vector, after computing the centroid-relative unit vector o',
// we find the integer grid vector ȳ ∈ {-7.5, -6.5, ..., 7.5}^D that maximizes
// cosine similarity with o'. This uses the critical-rescaling-factor sweep from
// Algorithm 1 of the RaBitQ v2 paper.
//
// Note: we assume that the caller applies the random orthogonal transformation,
// so no need to do it here.
func (q *RaBitQuantizer) quantizeHelper(
	w *workspace.T, qs *RaBitQuantizedVectorSet, vectors vector.Set,
) {
	if buildutil.CrdbTestBuild && q.distanceMetric == vecpb.CosineDistance {
		utils.ValidateUnitVectors(vectors)
	}

	count := vectors.Count
	oldCount := qs.GetCount()
	qs.AddUndefined(count)

	if q.distanceMetric != vecpb.L2SquaredDistance {
		centroidDotProducts := qs.CentroidDotProducts[oldCount:]
		for i := range count {
			centroidDotProducts[i] = num32.Dot(vectors.At(i), qs.Centroid)
		}
	}

	tempVectors := w.AllocVectorSet(count, q.dims)
	defer w.FreeVectorSet(tempVectors)

	// Compute centroid-relative vectors: o_raw - c.
	tempDiffs := tempVectors
	for i := range count {
		num32.SubTo(tempDiffs.At(i), vectors.At(i), qs.Centroid)
	}

	// Compute Euclidean distances from each vector to the centroid.
	centroidDistances := qs.CentroidDistances[oldCount:]
	for i := range count {
		centroidDistances[i] = num32.Norm(tempDiffs.At(i))
	}

	// Normalize to unit vectors: o' = (o_raw - c) / ||o_raw - c||.
	tempUnitVectors := tempDiffs
	for i := range count {
		if centroidDistances[i] != 0 {
			num32.ScaleTo(
				tempUnitVectors.At(i), 1.0/centroidDistances[i], tempUnitVectors.At(i),
			)
		}
	}

	dotProducts := qs.QuantizedDotProducts[oldCount:]
	codeNorms := qs.CodeNorms[oldCount:]

	// Temporary storage for grid values and critical factors, reused across
	// vectors. Also store "best" grid values to avoid O(N^2) rollback.
	gridValues := make([]float32, q.dims)
	bestGridValues := make([]float32, q.dims)
	critFactors := make([]critFactor, 0, q.dims*8)

	for vecIdx := range count {
		unitVec := tempUnitVectors.At(vecIdx)
		code := qs.Codes.At(oldCount + vecIdx)

		q.findOptimalGrid(unitVec, gridValues, bestGridValues, &critFactors)

		// Compute ||ȳ|| and <ȳ/||ȳ||, o'>.
		var yNormSq float64
		var yDotO float64
		for dim := range q.dims {
			g := float64(bestGridValues[dim])
			yNormSq += g * g
			yDotO += g * float64(unitVec[dim])
		}
		yNorm := math.Sqrt(yNormSq)
		codeNorms[vecIdx] = float32(yNorm)

		// Store inverted dot product: 1 / <ȳ/||ȳ||, o'>.
		// This equals ||ȳ|| / <ȳ, o'>.
		if yNorm > 0 && yDotO != 0 {
			dotProducts[vecIdx] = float32(yNorm / yDotO)
		} else {
			dotProducts[vecIdx] = 0
		}

		// Pack the grid vector as 4-bit unsigned nibbles into uint64s.
		// Unsigned form: ȳ_u[i] = ȳ[i] + 7.5, giving values in {0,1,...,15}.
		// Big-endian nibble order: first dimension in the most-significant nibble.
		for j := range code {
			code[j] = 0
		}
		for dim := range q.dims {
			yu := uint64(bestGridValues[dim] + 7.5)
			wordIdx := dim / 16
			nibblePos := 60 - (dim%16)*4
			code[wordIdx] |= yu << uint(nibblePos)
		}
	}
}

// findOptimalGrid finds the grid vector ȳ ∈ {-7.5, -6.5, ..., 7.5}^D that
// maximizes cosine similarity with the unit vector o'. This implements
// Algorithm 1 from the RaBitQ v2 paper: initialize each dimension to
// sign(o'[i]) * 0.5, then sweep through critical rescaling factors.
//
// gridValues is scratch space. bestGridValues receives the optimal grid vector.
// critFactors is a reusable buffer for the critical factor list.
func (q *RaBitQuantizer) findOptimalGrid(
	unitVec vector.T, gridValues, bestGridValues []float32, critFactors *[]critFactor,
) {
	// Initialize: ȳ[i] = sign(o'[i]) * 0.5.
	var dotProduct float64
	var normSq float64
	*critFactors = (*critFactors)[:0]
	for dim := range q.dims {
		oPrime := float64(unitVec[dim])
		if oPrime >= 0 {
			gridValues[dim] = 0.5
		} else {
			gridValues[dim] = -0.5
		}
		dotProduct += oPrime * float64(gridValues[dim])
		normSq += float64(gridValues[dim]) * float64(gridValues[dim])

		if oPrime == 0 {
			continue
		}
		sign := float64(1)
		if oPrime < 0 {
			sign = -1
		}
		// For each grid point {1.5, 2.5, ..., 7.5} in the same direction as
		// o'[i], compute the critical rescaling factor t = gridVal / o'[i].
		for level := 1; level <= 7; level++ {
			newVal := float32(sign * (float64(level) + 0.5))
			t := float64(newVal) / oPrime
			if t > 0 {
				*critFactors = append(*critFactors, critFactor{
					t: t, dim: dim, newVal: newVal,
				})
			}
		}
	}

	// Sort critical factors by ascending rescaling factor.
	slices.SortFunc(*critFactors, func(a, b critFactor) int {
		if a.t < b.t {
			return -1
		}
		if a.t > b.t {
			return 1
		}
		return 0
	})

	// Sweep through critical factors, incrementally updating dotProduct
	// and normSq. Track the configuration that maximizes cosine similarity.
	// Save a snapshot of gridValues at the best point to avoid rollback.
	bestCosine := dotProduct / math.Sqrt(normSq)
	copy(bestGridValues, gridValues)

	for _, cf := range *critFactors {
		dim := cf.dim
		oldVal := float64(gridValues[dim])
		newVal := float64(cf.newVal)

		if newVal == oldVal {
			continue
		}

		oPrime := float64(unitVec[dim])
		dotProduct += (newVal - oldVal) * oPrime
		normSq += newVal*newVal - oldVal*oldVal
		gridValues[dim] = cf.newVal

		if normSq > 0 {
			cosine := dotProduct / math.Sqrt(normSq)
			if cosine > bestCosine {
				bestCosine = cosine
				copy(bestGridValues, gridValues)
			}
		}
	}
}

func allocCodes(w *workspace.T, count, width int) RaBitQCodeSet {
	tempUints := w.AllocUint64s(count * width)
	return MakeRaBitQCodeSetFromRawData(tempUints, width)
}

func freeCodes(w *workspace.T, codeSet RaBitQCodeSet) {
	w.FreeUint64s(codeSet.Data)
}
