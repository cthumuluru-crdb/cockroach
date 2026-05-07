// Copyright 2025 The Cockroach Authors.
//
// Use of this software is governed by the CockroachDB Software License
// included in the /LICENSE file.

//go:build !arm64

package quantize

import "math/bits"

// bitProduct computes the weighted bit product used in RaBitQ distance
// estimation:
//
//	1*popcount(code&q1) + 2*popcount(code&q2) +
//	4*popcount(code&q3) + 8*popcount(code&q4)
func bitProduct(code, q1, q2, q3, q4 []uint64) int {
	var result int
	for j := range len(code) {
		result += 1 * bits.OnesCount64(code[j]&q1[j])
		result += 2 * bits.OnesCount64(code[j]&q2[j])
		result += 4 * bits.OnesCount64(code[j]&q3[j])
		result += 8 * bits.OnesCount64(code[j]&q4[j])
	}
	return result
}
