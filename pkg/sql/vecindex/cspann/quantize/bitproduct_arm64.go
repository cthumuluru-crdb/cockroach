// Copyright 2025 The Cockroach Authors.
//
// Use of this software is governed by the CockroachDB Software License
// included in the /LICENSE file.

//go:build arm64

package quantize

import (
	"math/bits"
	"unsafe"
)

// bitProduct computes the weighted bit product used in RaBitQ distance
// estimation:
//
//	1*popcount(code&q1) + 2*popcount(code&q2) +
//	4*popcount(code&q3) + 8*popcount(code&q4)
//
// On ARM64, this uses NEON instructions (CNT, UADDLP, UADALP) to
// process 2 uint64s per iteration. Any remaining odd element is
// handled with scalar code.
func bitProduct(code, q1, q2, q3, q4 []uint64) int {
	n := len(code)
	pairs := n / 2
	var result int
	if pairs > 0 {
		result = bitProductNEON(
			unsafe.Pointer(&code[0]),
			unsafe.Pointer(&q1[0]),
			unsafe.Pointer(&q2[0]),
			unsafe.Pointer(&q3[0]),
			unsafe.Pointer(&q4[0]),
			pairs,
		)
	}
	if n&1 != 0 {
		j := n - 1
		result += 1 * bits.OnesCount64(code[j]&q1[j])
		result += 2 * bits.OnesCount64(code[j]&q2[j])
		result += 4 * bits.OnesCount64(code[j]&q3[j])
		result += 8 * bits.OnesCount64(code[j]&q4[j])
	}
	return result
}

// bitProductNEON computes the weighted bit product over pairs of
// uint64 elements using ARM64 NEON SIMD instructions. It processes
// 2 uint64s (128 bits) per loop iteration.
//
//go:noescape
func bitProductNEON(code, q1, q2, q3, q4 unsafe.Pointer, pairs int) int
