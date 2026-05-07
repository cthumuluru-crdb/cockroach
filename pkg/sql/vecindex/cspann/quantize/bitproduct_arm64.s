// Copyright 2025 The Cockroach Authors.
//
// Use of this software is governed by the CockroachDB Software License
// included in the /LICENSE file.

//go:build arm64

#include "textflag.h"

// bitProductNEON computes the weighted popcount:
//   result = 1*popcount(code&q1) + 2*popcount(code&q2) +
//            4*popcount(code&q3) + 8*popcount(code&q4)
//
// Uses NEON CNT (byte popcount), UADDLP (pairwise add bytes to
// halfwords), and UADALP (pairwise add-accumulate halfwords to words)
// to process 128 bits (2 uint64s) per iteration.
//
// Register usage:
//   R0-R4: input pointers (code, q1, q2, q3, q4), post-incremented
//   R5:    loop counter (pairs remaining)
//   V0-V3: NEON accumulators for q1, q2, q3, q4 popcounts
//   V4-V8: temporaries for loads and computation
//   R6-R9: scalar results after horizontal reduction
//
// func bitProductNEON(code, q1, q2, q3, q4 unsafe.Pointer, pairs int) int
TEXT ·bitProductNEON(SB), NOSPLIT, $0-56
	MOVD code+0(FP), R0
	MOVD q1+8(FP), R1
	MOVD q2+16(FP), R2
	MOVD q3+24(FP), R3
	MOVD q4+32(FP), R4
	MOVD pairs+40(FP), R5

	// Zero NEON accumulators.
	WORD $0x6f00e400 // movi v0.2d, #0
	WORD $0x6f00e401 // movi v1.2d, #0
	WORD $0x6f00e402 // movi v2.2d, #0
	WORD $0x6f00e403 // movi v3.2d, #0

	CBZ R5, reduce

loop:
	// Load 2 uint64s (128 bits) from each slice, post-increment by 16.
	WORD $0x3cc10404 // ldr q4, [x0], #16  ; code
	WORD $0x3cc10425 // ldr q5, [x1], #16  ; q1
	WORD $0x3cc10446 // ldr q6, [x2], #16  ; q2
	WORD $0x3cc10467 // ldr q7, [x3], #16  ; q3
	WORD $0x3cc10488 // ldr q8, [x4], #16  ; q4

	// popcount(code & q1) → accumulate into v0.
	WORD $0x4e241ca5 // and.16b v5, v5, v4
	WORD $0x4e2058a5 // cnt.16b v5, v5
	WORD $0x6e2028a5 // uaddlp.8h v5, v5
	WORD $0x6e6068a0 // uadalp.4s v0, v5

	// popcount(code & q2) → accumulate into v1.
	WORD $0x4e241cc6 // and.16b v6, v6, v4
	WORD $0x4e2058c6 // cnt.16b v6, v6
	WORD $0x6e2028c6 // uaddlp.8h v6, v6
	WORD $0x6e6068c1 // uadalp.4s v1, v6

	// popcount(code & q3) → accumulate into v2.
	WORD $0x4e241ce7 // and.16b v7, v7, v4
	WORD $0x4e2058e7 // cnt.16b v7, v7
	WORD $0x6e2028e7 // uaddlp.8h v7, v7
	WORD $0x6e6068e2 // uadalp.4s v2, v7

	// popcount(code & q4) → accumulate into v3.
	WORD $0x4e241d04 // and.16b v4, v8, v4
	WORD $0x4e205884 // cnt.16b v4, v4
	WORD $0x6e202884 // uaddlp.8h v4, v4
	WORD $0x6e606883 // uadalp.4s v3, v4

	SUB $1, R5, R5
	CBNZ R5, loop

reduce:
	// Horizontal sum each accumulator to a scalar.
	WORD $0x4eb1b800 // addv.4s s0, v0
	WORD $0x1e260006 // fmov w6, s0
	WORD $0x4eb1b821 // addv.4s s1, v1
	WORD $0x1e260027 // fmov w7, s1
	WORD $0x4eb1b842 // addv.4s s2, v2
	WORD $0x1e260048 // fmov w8, s2
	WORD $0x4eb1b863 // addv.4s s3, v3
	WORD $0x1e260069 // fmov w9, s3

	// result = 1*R6 + 2*R7 + 4*R8 + 8*R9
	ADD R7<<1, R6
	ADD R8<<2, R6
	ADD R9<<3, R6

	MOVD R6, ret+48(FP)
	RET
