// Package embed holds the BM25 retrieval index (spec.md §5.5, §14).
//
// v1.34 retrieval is bm25-only-v1; no external embedding model is invoked.
// No external retrieval library is used; BM25 is hand-rolled in-tree per the
// model-free spec constraint (bd memory gum-spec-v1-34-model-free-spec-v1).
// Only stdlib + existing transitive dependencies are permitted.
//
// On-disk format:
//
//	dir/
//	  index.json  — {"version":"bm25-only-v1","params":{...},"docs":[...],"df":{...}}
//
// The format is deterministic: docs entries are sorted by op_id lexicographically;
// df keys are sorted alphabetically; docs[].tokens keys are sorted alphabetically.
package embed

import (
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"unicode"

	"github.com/ehmo/gum/internal/catalog"
	"github.com/ehmo/gum/internal/fsatomic"
)

// BM25 parameters
const (
	bm25K1 = 1.5
	bm25B  = 0.75
)

// English stopwords (25-word list from spec)
var stopwords = map[string]bool{
	"a": true, "an": true, "the": true, "of": true, "to": true,
	"in": true, "on": true, "for": true, "at": true, "by": true,
	"with": true, "from": true, "as": true, "is": true, "are": true,
	"was": true, "were": true, "be": true, "been": true, "being": true,
	"and": true, "or": true, "not": true, "but": true, "if": true,
	"then": true,
}

// indexDoc is a single document in the index (one per op).
type indexDoc struct {
	OpID         string         `json:"op_id"`
	Tokens       map[string]int `json:"tokens"`
	Length       int            `json:"length"`
	Summary      string         `json:"summary"`
	RiskClass    string         `json:"risk_class"`
	AuthStrategy string         `json:"auth_strategy"`
}

// indexParams holds BM25 index-level statistics.
type indexParams struct {
	K1    float64 `json:"k1"`
	B     float64 `json:"b"`
	AvgDL float64 `json:"avg_dl"`
	N     int     `json:"n"`
}

// indexFile is the on-disk JSON structure.
type indexFile struct {
	Version string         `json:"version"`
	Params  indexParams    `json:"params"`
	Docs    []indexDoc     `json:"docs"`
	DF      map[string]int `json:"df"`
}

// Index is an immutable BM25 keyword index over a catalog snapshot.
type Index struct {
	params indexParams
	docs   []indexDoc
	df     map[string]int
}

// SearchResult is a single hit: op_id, score, and the variant_id of the
// op's default variant (for risk_class/auth_strategy lookup by the caller).
type SearchResult struct {
	OpID         string  `json:"op_id"`
	Score        float64 `json:"score"`
	Summary      string  `json:"summary"`
	RiskClass    string  `json:"risk_class"`
	AuthStrategy string  `json:"auth_strategy"`
}

// Build indexes every op's title, summary, op_id (tokenized), and variant
// descriptions from the catalog snapshot. Returns an Index ready for Search.
// Errors: ErrCatalogEmpty, ErrBuildFailed.
func Build(cat *catalog.Catalog) (*Index, error) {
	if len(cat.Ops) == 0 {
		return nil, ErrCatalogEmpty
	}

	docs := make([]indexDoc, 0, len(cat.Ops))

	for _, op := range cat.Ops {
		// Build document text from op fields
		var sb strings.Builder
		sb.WriteString(op.Title)
		sb.WriteString(" ")
		sb.WriteString(op.Summary)
		sb.WriteString(" ")
		sb.WriteString(op.OpID)

		// Add variant aliases/examples
		for _, v := range op.Variants {
			sb.WriteString(" ")
			sb.WriteString(v.VariantID)
		}

		tokens := tokenize(sb.String())
		termFreq := make(map[string]int, len(tokens))
		for _, t := range tokens {
			termFreq[t]++
		}

		// Find default variant for risk_class and auth_strategy
		riskClass := ""
		authStrategy := ""
		for _, v := range op.Variants {
			if v.VariantID == op.DefaultVariantID {
				riskClass = string(v.RiskClass)
				authStrategy = string(v.AuthStrategy)
				break
			}
		}

		docs = append(docs, indexDoc{
			OpID:         op.OpID,
			Tokens:       termFreq,
			Length:       len(tokens),
			Summary:      op.Summary,
			RiskClass:    riskClass,
			AuthStrategy: authStrategy,
		})
	}

	// Sort docs by op_id for determinism
	sort.Slice(docs, func(i, j int) bool {
		return docs[i].OpID < docs[j].OpID
	})

	// Compute document frequencies
	df := make(map[string]int)
	for _, doc := range docs {
		for term := range doc.Tokens {
			df[term]++
		}
	}

	// Compute avgDL
	totalLen := 0
	for _, doc := range docs {
		totalLen += doc.Length
	}
	avgDL := float64(totalLen) / float64(len(docs))

	return &Index{
		params: indexParams{
			K1:    bm25K1,
			B:     bm25B,
			AvgDL: avgDL,
			N:     len(docs),
		},
		docs: docs,
		df:   df,
	}, nil
}

// Search returns the top-K hits ranked by BM25 score, descending.
// topK is clamped to [1, 50]; 0 or negative defaults to 10.
func (i *Index) Search(query string, topK int) []SearchResult {
	if topK <= 0 {
		topK = 10
	}
	if topK > 50 {
		topK = 50
	}

	queryTokens := tokenize(query)
	if len(queryTokens) == 0 {
		return nil
	}

	// Deduplicate query tokens
	seen := make(map[string]bool)
	uniqueTokens := queryTokens[:0]
	for _, t := range queryTokens {
		if !seen[t] {
			seen[t] = true
			uniqueTokens = append(uniqueTokens, t)
		}
	}

	N := float64(i.params.N)
	results := make([]SearchResult, 0)

	for _, doc := range i.docs {
		score := 0.0
		docLen := float64(doc.Length)

		for _, term := range uniqueTokens {
			df := float64(i.df[term])
			if df == 0 {
				continue
			}
			f := float64(doc.Tokens[term])
			if f == 0 {
				continue
			}

			// IDF with smoothing
			idf := math.Log((N-df+0.5)/(df+0.5) + 1)

			// BM25 TF component
			norm := f * (i.params.K1 + 1) / (f + i.params.K1*(1-i.params.B+i.params.B*docLen/i.params.AvgDL))
			score += idf * norm
		}

		if score > 0 {
			results = append(results, SearchResult{
				OpID:         doc.OpID,
				Score:        score,
				Summary:      doc.Summary,
				RiskClass:    doc.RiskClass,
				AuthStrategy: doc.AuthStrategy,
			})
		}
	}

	// Sort: descending score, ties by op_id ascending
	sort.Slice(results, func(a, b int) bool {
		if results[a].Score != results[b].Score {
			return results[a].Score > results[b].Score
		}
		return results[a].OpID < results[b].OpID
	})

	if len(results) > topK {
		results = results[:topK]
	}

	return results
}

// OpIDs returns the op_ids indexed by this Index, sorted lexicographically.
// Returned slice is a fresh copy; callers may mutate freely.
func (i *Index) OpIDs() []string {
	out := make([]string, 0, len(i.docs))
	for _, d := range i.docs {
		out = append(out, d.OpID)
	}
	return out
}

// Save serialises the index to a directory. The on-disk format is documented
// in the package comment above. The directory is created if it does not exist.
func (i *Index) Save(dir string) error {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("embed: create index dir: %w", err)
	}

	// Build sorted docs with sorted token maps
	docs := make([]indexDoc, len(i.docs))
	for idx, doc := range i.docs {
		sortedTokens := make(map[string]int, len(doc.Tokens))
		for k, v := range doc.Tokens {
			sortedTokens[k] = v
		}
		docs[idx] = indexDoc{
			OpID:         doc.OpID,
			Tokens:       sortedTokens,
			Length:       doc.Length,
			Summary:      doc.Summary,
			RiskClass:    doc.RiskClass,
			AuthStrategy: doc.AuthStrategy,
		}
	}

	// Build sorted df map
	sortedDF := make(map[string]int, len(i.df))
	for k, v := range i.df {
		sortedDF[k] = v
	}

	file := indexFile{
		Version: "bm25-only-v1",
		Params:  i.params,
		Docs:    docs,
		DF:      sortedDF,
	}

	data, err := marshalDeterministic(file)
	if err != nil {
		return fmt.Errorf("embed: marshal index: %w", err)
	}

	path := filepath.Join(dir, "index.json")
	// Atomic write so a crash mid-write can't leave a corrupt index.json that
	// fails to parse on the next load (review gum-yz76).
	if err := fsatomic.WriteFile(path, data, 0o644); err != nil {
		return fmt.Errorf("embed: write index.json: %w", err)
	}

	return nil
}

// Load reads a previously-saved index from dir.
func Load(dir string) (*Index, error) {
	path := filepath.Join(dir, "index.json")
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("embed: read index.json: %w", err)
	}

	var file indexFile
	if err := json.Unmarshal(data, &file); err != nil {
		return nil, fmt.Errorf("%w: %v", ErrIndexCorrupt, err)
	}

	if file.Version != "bm25-only-v1" {
		return nil, fmt.Errorf("%w: version %q not supported", ErrIndexCorrupt, file.Version)
	}

	if file.Docs == nil {
		return nil, fmt.Errorf("%w: missing docs", ErrIndexCorrupt)
	}

	return &Index{
		params: file.Params,
		docs:   file.Docs,
		df:     file.DF,
	}, nil
}

var (
	// ErrCatalogEmpty is returned by Build when the catalog has zero ops.
	ErrCatalogEmpty = errors.New("BM25_CATALOG_EMPTY")
	// ErrBuildFailed is returned by Build on internal processing failure.
	ErrBuildFailed = errors.New("BM25_BUILD_FAILED")
	// ErrIndexCorrupt is returned by Load when the on-disk data fails validation.
	ErrIndexCorrupt = errors.New("BM25_INDEX_CORRUPT")
)

// tokenize lowercases, splits on non-letter/non-digit, drops short tokens,
// removes stopwords, and applies a minimal stemmer.
func tokenize(text string) []string {
	lower := strings.ToLower(text)

	// Split on non-letter, non-digit characters
	words := strings.FieldsFunc(lower, func(r rune) bool {
		return !unicode.IsLetter(r) && !unicode.IsDigit(r)
	})

	result := make([]string, 0, len(words))
	for _, w := range words {
		if len(w) < 2 {
			continue
		}
		if stopwords[w] {
			continue
		}
		w = stem(w)
		if len(w) < 2 {
			continue
		}
		result = append(result, w)
	}

	return result
}

// stem applies minimal suffix stripping: s, ing, ed, ly.
// The remaining stem must be >=3 chars.
func stem(word string) string {
	suffixes := []string{"ing", "ed", "ly", "s"}
	for _, suf := range suffixes {
		if strings.HasSuffix(word, suf) {
			stem := word[:len(word)-len(suf)]
			if len(stem) >= 3 {
				if suf == "s" && strings.HasSuffix(stem, "s") {
					return word
				}
				if (suf == "ing" || suf == "ed") && hasDoubledFinalLetter(stem) {
					return word
				}
				return stem
			}
		}
	}
	return word
}

func hasDoubledFinalLetter(s string) bool {
	if len(s) < 2 {
		return false
	}
	last := rune(s[len(s)-1])
	prev := rune(s[len(s)-2])
	return last == prev && unicode.IsLetter(last)
}

// marshalDeterministic marshals indexFile with sorted map keys.
// We use a custom approach to ensure deterministic key ordering.
func marshalDeterministic(f indexFile) ([]byte, error) {
	// Use encoding/json which sorts map keys alphabetically for string-keyed maps
	// as of Go 1.12+
	return json.Marshal(f)
}
