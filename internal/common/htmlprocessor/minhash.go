package htmlprocessor

import (
	"math"

	"github.com/cespare/xxhash/v2"
)

const (
	// minhashSignatureSize is the number of slots in a page fingerprint.
	// FROZEN: changing it makes new fingerprints incomparable with stored ones.
	minhashSignatureSize = 24

	// minhashShingleSize is the number of consecutive words per feature.
	// FROZEN: changing it changes the feature set and every fingerprint with it.
	minhashShingleSize = 3

	// minhashValueMask keeps the low 32 bits of a slot. The stored width is part
	// of the format.
	// FROZEN: widening or narrowing it breaks comparability with stored values.
	minhashValueMask = 0xFFFFFFFF

	// minhashShingleSeparator joins the words of a shingle.
	// FROZEN: it is hashed together with the words.
	minhashShingleSeparator = ' '

	// minhashShingleBufferSize preallocates the shingle buffer for typical word
	// lengths; longer shingles simply grow it once.
	minhashShingleBufferSize = 64
)

// minhashPermutations are the fixed (a, b) pairs for the 24 signature slots.
// Multipliers are odd so that the multiply stays a bijection on 64-bit values.
// FROZEN: changing any value invalidates comparability with all stored fingerprints.
var minhashPermutations = [minhashSignatureSize][2]uint64{
	{0xa4c98df7791d34f5, 0xf29cdf8339f8fedf},
	{0x0774856caa30d7bd, 0xd7f0abacdac9191b},
	{0xa18f65698ef85ecf, 0x79b082d44aab66c4},
	{0x6b962b1e44f73461, 0xc81488f969615540},
	{0x7c89db0a772c3469, 0x1cae6ef776f57343},
	{0x706db994d9e4479f, 0xab3c3c0bd2faeee0},
	{0x9d01937ea587b091, 0xb8a63c260f208eba},
	{0x233f6849990f30cd, 0xa889a76cd8f28ecc},
	{0x26575e22bb2cb0a3, 0x0bcf38c74d657f14},
	{0x5cd68c693ce5bfbd, 0xec49358fa358c242},
	{0x9fc2be5f716d4bdf, 0x7b4bbb2463da887d},
	{0x93ddba79cb6cefdf, 0xdd84809b4a1ebbdd},
	{0xa693d69d0c9ce0eb, 0xaa07f1bfc5264256},
	{0x16b1b59cdb85b37b, 0x2dadf305921ee8af},
	{0x7b2b66fbbb445ffb, 0xdc94f63238c01784},
	{0xe52b92fcc276f3e9, 0x531ec64d02c62209},
	{0xe11733fe964fee5d, 0x1b0a187d647fe07d},
	{0xba9e129a237d2881, 0x977ee429241b3495},
	{0x4dc26f00186ff481, 0x61d1fb2ee8d38fdb},
	{0xbbf9e98ab467b7b9, 0xaad515588bddfd82},
	{0x60237982341f258d, 0xaac11b6877aa96b8},
	{0x13746c3bba7ef803, 0x65141e945dbc0680},
	{0xce281be13ebde619, 0x8bb2be8114ab88ed},
	{0x7b0e408a9640f20d, 0x2e3b3547d063234b},
}

// computePageMinHash builds the MinHash fingerprint of a body word stream: one
// slot per permutation, each holding the minimum permuted hash over all 3-word
// shingles, truncated to 32 bits. Returns nil when the text is too short to form
// a single shingle, which leaves the field out of the serialized event.
//
// Duplicate shingles need no deduplication: the minimum is idempotent, so the
// multiset and the set produce the same signature.
//
// FROZEN: the shingle bytes, the base hash (xxhash64), the permutation
// arithmetic and the truncation together define the on-the-wire fingerprint.
// Any change makes fingerprints computed before and after incomparable.
func computePageMinHash(words []string) []uint64 {
	if len(words) < minhashShingleSize {
		return nil
	}

	signature := make([]uint64, minhashSignatureSize)
	for i := range signature {
		signature[i] = math.MaxUint64
	}

	buf := make([]byte, 0, minhashShingleBufferSize)
	for start := 0; start+minhashShingleSize <= len(words); start++ {
		// Resetting the length (not just overwriting) is what keeps a longer
		// previous shingle from leaving trailing bytes inside the hashed slice.
		buf = buf[:0]
		for offset := 0; offset < minhashShingleSize; offset++ {
			if offset > 0 {
				buf = append(buf, minhashShingleSeparator)
			}
			buf = append(buf, words[start+offset]...)
		}

		h := xxhash.Sum64(buf)
		for i, permutation := range minhashPermutations {
			permuted := permutation[0]*h + permutation[1]
			if permuted < signature[i] {
				signature[i] = permuted
			}
		}
	}

	// Truncation happens after the minimum so that the ordering used to select
	// the winning shingle is the full 64-bit one.
	for i := range signature {
		signature[i] &= minhashValueMask
	}

	return signature
}
