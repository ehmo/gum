// upstream_vectors_test.go — the RFC 8785 reference corpus.
//
// internal/output/jcs used to be checked only against golden files written by
// hand from a reading of the spec, so a misreading of the spec produced a golden
// file that agreed with it. Two conformance bugs survived that way: numbers went
// through Go's 'g' verb instead of the ECMAScript algorithm (bead gum-xvy9), and
// every control character was escaped as \uXXXX where RFC 8785 mandates \b, \t,
// \n, \f and \r (bead gum-gq9q). Both were found by the corpus below, not by the
// golden files.
//
// The tests here are in package jcs so the number test can call es6Number
// directly. Marshal's integer fast path would otherwise intercept every integral
// vector before the ES6 formatter saw it.
package jcs

import (
	"bytes"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"math"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"testing"
)

// upstreamDir holds the twelve vendored vector files. testdata/upstream/README.md
// records the upstream commit and the license.
const upstreamDir = "testdata/upstream"

// TestUpstreamJCSVectors canonicalizes every input/<name>.json and requires the
// result to equal output/<name>.json byte for byte. The expected files carry no
// trailing newline, so the comparison is on raw bytes with nothing trimmed.
func TestUpstreamJCSVectors(t *testing.T) {
	inputs, err := filepath.Glob(filepath.Join(upstreamDir, "input", "*.json"))
	if err != nil {
		t.Fatalf("glob %s: %v", upstreamDir, err)
	}
	sort.Strings(inputs)

	// Upstream ships six pairs. A vendored directory that lost files would
	// otherwise pass this test by testing nothing.
	const wantPairs = 6
	if len(inputs) != wantPairs {
		t.Fatalf("found %d input vectors in %s; want %d, see its README.md",
			len(inputs), upstreamDir, wantPairs)
	}

	for _, input := range inputs {
		name := filepath.Base(input)
		t.Run(strings.TrimSuffix(name, ".json"), func(t *testing.T) {
			raw, err := os.ReadFile(input)
			if err != nil {
				t.Fatalf("read %s: %v", input, err)
			}
			want, err := os.ReadFile(filepath.Join(upstreamDir, "output", name))
			if err != nil {
				t.Fatalf("read expected output for %s: %v", name, err)
			}

			got, err := canonicalizeRaw(raw)
			if err != nil {
				t.Fatalf("canonicalize %s: %v", name, err)
			}

			if string(got) != string(want) {
				t.Errorf("canonicalization of %s disagrees with the RFC 8785 corpus\ngot:  %s\nwant: %s\ngot hex:  %x\nwant hex: %x",
					name, got, want, got, want)
			}
		})
	}
}

// canonicalizeRaw decodes a JSON document and canonicalizes it, which is the
// shape the dispatcher uses: args arrive as JSON text, get decoded, and the
// resulting tree goes to Marshal (internal/dispatch/lifecycle.go canonicalizeArgs).
// UseNumber is what keeps a token's digits intact through the decode.
func canonicalizeRaw(raw []byte) ([]byte, error) {
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.UseNumber()

	var tree any
	if err := dec.Decode(&tree); err != nil {
		return nil, err
	}

	return Marshal(tree)
}

// es6VectorLines is how many lines of the upstream number sequence this test
// regenerates. Upstream's testdata/README.md publishes a SHA-256 for each of
// 1e3, 1e4, 1e5, 1e6, 1e7 and 1e8 lines. 1e4 is the smallest count that reaches
// past the 168 static values and the 2000 serial subnormals into the SHA-256
// chained random tail, so it exercises all three generator arms.
const es6VectorLines = 10000

// es6VectorSHA256 and es6VectorBytes are the published values for the first
// es6VectorLines lines, copied from upstream's testdata/README.md. They anchor
// the whole test: if upstreamAppendNumber below were not byte-exact with
// upstream's own formatter, this hash would not match, and the comparison
// against es6Number would be worthless.
const (
	es6VectorSHA256 = "b9f7a8e75ef22a835685a52ccba7f7d6bdc99e34b010992cbc5864cd12be6892"
	es6VectorBytes  = 399022
)

// TestUpstreamES6NumberVectors regenerates the head of upstream's 100-million
// line ES6 number corpus and compares es6Number against it.
//
// The corpus is a 4 GB generated file, so it is not vendored. Upstream publishes
// the generator and a checksum per line count precisely so an implementation can
// reproduce it offline; testdata/upstream/README.md quotes that intent.
//
// This is a differential test between two independent algorithms. Upstream's
// formatter switches strconv between the 'f' and 'e' verbs at 1e-6 and 1e21 and
// then strips one zero from a padded negative exponent. es6Number instead walks
// the five ECMA-262 Number::toString rules over the shortest digit string and its
// decimal point position. They share no code and agree on all 10000 values.
func TestUpstreamES6NumberVectors(t *testing.T) {
	h := sha256.New()
	next := upstreamFloatSequence()

	var line []byte
	var size int
	var mismatches int

	for n := 1; n <= es6VectorLines; n++ {
		f := next()

		line = strconv.AppendUint(line[:0], math.Float64bits(f), 16)
		line = append(line, ',')
		line = upstreamAppendNumber(line, f)
		want := string(line[strings.IndexByte(string(line), ',')+1:])
		line = append(line, '\n')

		h.Write(line)
		size += len(line)

		if got := es6Number(f); got != want {
			// Cap the output: a broken es6Number would otherwise print
			// thousands of lines and bury the checksum result below.
			if mismatches < 10 {
				t.Errorf("line %d: es6Number(%x) = %q; corpus says %q",
					n, math.Float64bits(f), got, want)
			}
			mismatches++
		}
	}

	if size != es6VectorBytes {
		t.Errorf("regenerated %d bytes for %d lines; upstream README says %d",
			size, es6VectorLines, es6VectorBytes)
	}
	if got := hex.EncodeToString(h.Sum(nil)); got != es6VectorSHA256 {
		t.Fatalf("regenerated corpus SHA-256 = %s; upstream README says %s. "+
			"The generator or the reference formatter transcribed below has drifted "+
			"from upstream, so the comparison above proves nothing.",
			got, es6VectorSHA256)
	}
	if mismatches > 10 {
		t.Errorf("%d of %d vectors disagree; only the first 10 are listed",
			mismatches, es6VectorLines)
	}
}

// upstreamFloatSequence reproduces the float64 sequence of upstream's
// testdata/numgen.go. Three arms in order: 168 hand-picked edge cases, 2000
// consecutive bit patterns from the smallest normal upward, then an endless
// SHA-256 chain filtered to finite non-zero values.
func upstreamFloatSequence() func() float64 {
	static := [...]uint64{
		0x0000000000000000, 0x8000000000000000, 0x0000000000000001, 0x8000000000000001,
		0xc46696695dbd1cc3, 0xc43211ede4974a35, 0xc3fce97ca0f21056, 0xc3c7213080c1a6ac,
		0xc39280f39a348556, 0xc35d9b1f5d20d557, 0xc327af4c4a80aaac, 0xc2f2f2a36ecd5556,
		0xc2be51057e155558, 0xc28840d131aaaaac, 0xc253670dc1555557, 0xc21f0b4935555557,
		0xc1e8d5d42aaaaaac, 0xc1b3de4355555556, 0xc17fca0555555556, 0xc1496e6aaaaaaaab,
		0xc114585555555555, 0xc0e046aaaaaaaaab, 0xc0aa0aaaaaaaaaaa, 0xc074d55555555555,
		0xc040aaaaaaaaaaab, 0xc00aaaaaaaaaaaab, 0xbfd5555555555555, 0xbfa1111111111111,
		0xbf6b4e81b4e81b4f, 0xbf35d867c3ece2a5, 0xbf0179ec9cbd821e, 0xbecbf647612f3696,
		0xbe965e9f80f29212, 0xbe61e54c672874db, 0xbe2ca213d840baf8, 0xbdf6e80fe033c8c6,
		0xbdc2533fe68fd3d2, 0xbd8d51ffd74c861c, 0xbd5774ccac3d3817, 0xbd22c3d6f030f9ac,
		0xbcee0624b3818f79, 0xbcb804ea293472c7, 0xbc833721ba905bd3, 0xbc4ebe9c5db3c61e,
		0xbc18987d17c304e5, 0xbbe3ad30dfcf371d, 0xbbaf7b816618582f, 0xbb792f9ab81379bf,
		0xbb442615600f9499, 0xbb101e77800c76e1, 0xbad9ca58cce0be35, 0xbaa4a1e0a3e6fe90,
		0xba708180831f320d, 0xba3a68cd9e985016, 0x446696695dbd1cc3, 0x443211ede4974a35,
		0x43fce97ca0f21056, 0x43c7213080c1a6ac, 0x439280f39a348556, 0x435d9b1f5d20d557,
		0x4327af4c4a80aaac, 0x42f2f2a36ecd5556, 0x42be51057e155558, 0x428840d131aaaaac,
		0x4253670dc1555557, 0x421f0b4935555557, 0x41e8d5d42aaaaaac, 0x41b3de4355555556,
		0x417fca0555555556, 0x41496e6aaaaaaaab, 0x4114585555555555, 0x40e046aaaaaaaaab,
		0x40aa0aaaaaaaaaaa, 0x4074d55555555555, 0x4040aaaaaaaaaaab, 0x400aaaaaaaaaaaab,
		0x3fd5555555555555, 0x3fa1111111111111, 0x3f6b4e81b4e81b4f, 0x3f35d867c3ece2a5,
		0x3f0179ec9cbd821e, 0x3ecbf647612f3696, 0x3e965e9f80f29212, 0x3e61e54c672874db,
		0x3e2ca213d840baf8, 0x3df6e80fe033c8c6, 0x3dc2533fe68fd3d2, 0x3d8d51ffd74c861c,
		0x3d5774ccac3d3817, 0x3d22c3d6f030f9ac, 0x3cee0624b3818f79, 0x3cb804ea293472c7,
		0x3c833721ba905bd3, 0x3c4ebe9c5db3c61e, 0x3c18987d17c304e5, 0x3be3ad30dfcf371d,
		0x3baf7b816618582f, 0x3b792f9ab81379bf, 0x3b442615600f9499, 0x3b101e77800c76e1,
		0x3ad9ca58cce0be35, 0x3aa4a1e0a3e6fe90, 0x3a708180831f320d, 0x3a3a68cd9e985016,
		0x4024000000000000, 0x4014000000000000, 0x3fe0000000000000, 0x3fa999999999999a,
		0x3f747ae147ae147b, 0x3f40624dd2f1a9fc, 0x3f0a36e2eb1c432d, 0x3ed4f8b588e368f1,
		0x3ea0c6f7a0b5ed8d, 0x3e6ad7f29abcaf48, 0x3e35798ee2308c3a, 0x3ed539223589fa95,
		0x3ed4ff26cd5a7781, 0x3ed4f95a762283ff, 0x3ed4f8c60703520c, 0x3ed4f8b72f19cd0d,
		0x3ed4f8b5b31c0c8d, 0x3ed4f8b58d1c461a, 0x3ed4f8b5894f7f0e, 0x3ed4f8b588ee37f3,
		0x3ed4f8b588e47da4, 0x3ed4f8b588e3849c, 0x3ed4f8b588e36bb5, 0x3ed4f8b588e36937,
		0x3ed4f8b588e368f8, 0x3ed4f8b588e368f1, 0x3ff0000000000000, 0xbff0000000000000,
		0xbfeffffffffffffa, 0xbfeffffffffffffb, 0x3feffffffffffffa, 0x3feffffffffffffb,
		0x3feffffffffffffc, 0x3feffffffffffffe, 0xbfefffffffffffff, 0xbfefffffffffffff,
		0x3fefffffffffffff, 0x3fefffffffffffff, 0x3fd3333333333332, 0x3fd3333333333333,
		0x3fd3333333333334, 0x0010000000000000, 0x000ffffffffffffd, 0x000fffffffffffff,
		0x7fefffffffffffff, 0xffefffffffffffff, 0x4340000000000000, 0xc340000000000000,
		0x4430000000000000, 0x44b52d02c7e14af5, 0x44b52d02c7e14af6, 0x44b52d02c7e14af7,
		0x444b1ae4d6e2ef4e, 0x444b1ae4d6e2ef4f, 0x444b1ae4d6e2ef50, 0x3eb0c6f7a0b5ed8c,
		0x3eb0c6f7a0b5ed8d, 0x41b3de4355555553, 0x41b3de4355555554, 0x41b3de4355555555,
		0x41b3de4355555556, 0x41b3de4355555557, 0xbecbf647612f3696, 0x43143ff3c1cb0959,
	}

	var state struct {
		idx   int
		data  []byte
		block [sha256.Size]byte
	}

	return func() float64 {
		const numSerial = 2000

		var f float64
		switch {
		case state.idx < len(static):
			f = math.Float64frombits(static[state.idx])
		case state.idx < len(static)+numSerial:
			f = math.Float64frombits(0x0010000000000000 + uint64(state.idx-len(static)))
		default:
			for f == 0 || math.IsNaN(f) || math.IsInf(f, 0) {
				if len(state.data) == 0 {
					state.block = sha256.Sum256(state.block[:])
					state.data = state.block[:]
				}
				f = math.Float64frombits(binary.LittleEndian.Uint64(state.data))
				state.data = state.data[8:]
			}
		}
		state.idx++

		return f
	}
}

// upstreamAppendNumber is upstream's own RFC 8785 §3.2.2.3 formatter, from
// testdata/numgen.go. It stands in for the corpus file's second column, so it
// must stay byte-exact with upstream; es6VectorSHA256 is what proves it is.
// Do not "improve" it. Its only local change is renaming upstream's `fmt`
// variable, which shadowed the package name.
func upstreamAppendNumber(b []byte, f float64) []byte {
	if f == 0 {
		f = 0 // normalize -0 as 0
	}

	verb := byte('f')
	if abs := math.Abs(f); abs != 0 && abs < 1e-6 || abs >= 1e21 {
		verb = 'e'
	}
	b = strconv.AppendFloat(b, f, verb, -1, 64)

	if verb == 'e' {
		n := len(b)
		if n >= 4 && b[n-4] == 'e' && b[n-3] == '-' && b[n-2] == '0' {
			b[n-2] = b[n-1]
			b = b[:n-1]
		}
	}

	return b
}
