// CLAUDE GENERATED
package exemplar

import (
	"math"
	"strings"
	"unicode"
)

// BM25 ranking parameters. Standard values from the Robertson/Walker
// IR literature; not tuned for our corpus because we don't have one
// big enough to tune against yet.
const (
	bm25K1 = 1.5  // term-frequency saturation
	bm25B  = 0.75 // doc-length normalization
)

// tokenize splits text into lowercase alphanumeric tokens. Unicode-
// aware (handles non-ASCII letters/digits) but doesn't stem or
// apply stopwords — the corpus is technical (GPQA-style prompts:
// "10^-4 eV", "E1, E2", etc.) where stopword lists hurt more than
// they help. If a future benchmark workload needs stopwords, swap
// in a stricter tokenizer here.
//
// Single-character tokens are dropped — they're almost never
// informative (filler letters, single digits) and they inflate
// scores when a long query happens to share a stop-letter with a
// short exemplar.
func tokenize(s string) []string {
	if s == "" {
		return nil
	}
	lower := strings.ToLower(s)
	raw := strings.FieldsFunc(lower, func(r rune) bool {
		return !unicode.IsLetter(r) && !unicode.IsDigit(r)
	})
	out := raw[:0]
	for _, t := range raw {
		if len(t) >= 2 {
			out = append(out, t)
		}
	}
	return out
}

// bm25Index pre-computes per-document tokens and corpus-wide term
// frequencies so scoring N queries against N docs is O(N²·avg_query_len)
// rather than O(N²·avg_doc_len). Built once per Retrieve call.
type bm25Index struct {
	// docs[i] = tokenized text of exemplar i.
	docs [][]string
	// docFreqs[term] = number of docs containing term at least once.
	docFreqs map[string]int
	// totalDocs is the corpus size.
	totalDocs int
	// avgDocLen is the mean token count across the corpus. Used by
	// the doc-length-normalization term in BM25.
	avgDocLen float64
}

// buildBM25Index tokenizes each exemplar's Prompt and assembles the
// corpus statistics. Output-text is intentionally NOT indexed — the
// query at warm-path time is a new prompt; matching against past
// outputs would surface exemplars whose model response was similar,
// not whose question was. Question-similarity is the right ranking
// signal for "show me past examples like this one."
func buildBM25Index(exemplars []Exemplar) *bm25Index {
	idx := &bm25Index{
		docs:     make([][]string, len(exemplars)),
		docFreqs: make(map[string]int),
	}
	totalLen := 0
	for i, e := range exemplars {
		toks := tokenize(e.Prompt)
		idx.docs[i] = toks
		totalLen += len(toks)
		seen := make(map[string]bool, len(toks))
		for _, t := range toks {
			if !seen[t] {
				idx.docFreqs[t]++
				seen[t] = true
			}
		}
	}
	idx.totalDocs = len(exemplars)
	if idx.totalDocs > 0 {
		idx.avgDocLen = float64(totalLen) / float64(idx.totalDocs)
	}
	return idx
}

// score computes the BM25 score of the query against document docIdx.
//
//	score = Σ_t∈Q [ IDF(t) · (tf(t,d) · (k1+1)) /
//	                          (tf(t,d) + k1 · (1 - b + b · |d|/avgdl)) ]
//
// IDF form: log(1 + (N - df + 0.5) / (df + 0.5))
// (the "BM25+1" smoothing variant that guarantees non-negative scores
//
//	even for terms appearing in every document).
//
// Returns 0 when the query has no terms or no terms intersect the doc.
func (idx *bm25Index) score(queryTokens []string, docIdx int) float64 {
	if idx.totalDocs == 0 || idx.avgDocLen == 0 || len(queryTokens) == 0 {
		return 0
	}
	doc := idx.docs[docIdx]
	if len(doc) == 0 {
		return 0
	}
	docLen := float64(len(doc))

	tf := make(map[string]int, len(doc))
	for _, t := range doc {
		tf[t]++
	}

	score := 0.0
	for _, qt := range queryTokens {
		f := float64(tf[qt])
		if f == 0 {
			continue
		}
		df := idx.docFreqs[qt]
		if df == 0 {
			continue // not in corpus; can happen if query mentions a novel term
		}
		idf := math.Log(1 + (float64(idx.totalDocs)-float64(df)+0.5)/(float64(df)+0.5))
		numerator := f * (bm25K1 + 1)
		denominator := f + bm25K1*(1-bm25B+bm25B*docLen/idx.avgDocLen)
		score += idf * numerator / denominator
	}
	return score
}

// rankedHit pairs an exemplar index with its BM25 score for sort.
type rankedHit struct {
	idx   int
	score float64
}
