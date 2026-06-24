package engine

import (
	"context"
	"sort"
	"strings"

	"github.com/asraygopa/gnar/internal/embed"
	"github.com/asraygopa/gnar/internal/model"
	"github.com/asraygopa/gnar/internal/store"
)

// RecallInput describes a recall query.
type RecallInput struct {
	Project     string
	Dir         string
	AllProjects bool
	Query       string
	Kinds       []model.Kind
	Tags        []string
	Limit       int
}

// Recall returns memories ranked by relevance to the query. With an empty query
// it returns the most recent memories. Ranking fuses a keyword score and a
// cosine-similarity score via Reciprocal Rank Fusion.
func (e *Engine) Recall(ctx context.Context, in RecallInput) ([]model.Memory, error) {
	limit := in.Limit
	if limit <= 0 {
		limit = 10
	}
	q := store.Query{
		Kinds:   in.Kinds,
		Tags:    in.Tags, // pushed into SQL so the cap applies to tag-matching rows
		Limit:   e.candidateCap(),
		OrderBy: "created_desc",
	}
	if !in.AllProjects {
		q.Project = ResolveProject(in.Project, in.Dir).ID
	}
	rows, err := e.store.Candidates(q)
	if err != nil {
		return nil, err
	}
	// Exact re-check in Go (SQL LIKE is a quote-delimited superset match).
	rows = filterByTags(rows, in.Tags)

	query := strings.TrimSpace(in.Query)
	if query == "" {
		// No query: most recent (rows already sorted desc), tagged with no score.
		out := make([]model.Memory, 0, min(limit, len(rows)))
		for i := 0; i < len(rows) && i < limit; i++ {
			out = append(out, rows[i].Memory)
		}
		return out, nil
	}

	ranked := e.rank(ctx, query, rows)
	if len(ranked) > limit {
		ranked = ranked[:limit]
	}
	return ranked, nil
}

// rank scores candidates by keyword + cosine and fuses via RRF.
func (e *Engine) rank(ctx context.Context, query string, rows []store.SearchRow) []model.Memory {
	n := len(rows)
	if n == 0 {
		return nil
	}

	// Keyword scores over all candidates.
	kwScores := make([]float64, n)
	qTerms := tokenize(query)
	for i, r := range rows {
		kwScores[i] = keywordScore(qTerms, r.Memory)
	}

	// Cosine scores — only for rows embedded by the *current* embedder, so the
	// vectors are comparable to the query vector.
	cosScores := make([]float64, n)
	haveVec := false
	// minCosine filters out weak similarities — important for the hash embedder,
	// where unrelated texts still share buckets and score a small nonzero cosine.
	const minCosine = 0.25
	if qvec, qid := e.tryEmbed(ctx, query); qid != "" {
		for i, r := range rows {
			if r.EmbedID == qid && len(r.Embedding) == len(qvec) {
				if c := embed.Cosine(qvec, r.Embedding); c >= minCosine {
					cosScores[i] = c
					haveVec = true
				}
			}
		}
	}

	kwRank := rankIndices(kwScores)
	fused := make([]scored, n)
	const k = 60.0
	for i := range rows {
		s := 0.0
		if kwScores[i] > 0 {
			s += 1.0 / (k + float64(kwRank[i]))
		}
		fused[i] = scored{idx: i, score: s}
	}
	if haveVec {
		cosRank := rankIndices(cosScores)
		for i := range rows {
			if cosScores[i] > 0 {
				fused[i].score += 1.0 / (k + float64(cosRank[i]))
			}
		}
	}

	sort.SliceStable(fused, func(a, b int) bool {
		if fused[a].score != fused[b].score {
			return fused[a].score > fused[b].score
		}
		// tie-break: more recent first
		return rows[fused[a].idx].Memory.CreatedAt.After(rows[fused[b].idx].Memory.CreatedAt)
	})

	out := make([]model.Memory, 0, n)
	for _, f := range fused {
		if f.score <= 0 {
			continue
		}
		m := rows[f.idx].Memory
		m.Score = f.score
		out = append(out, m)
	}
	return out
}

type scored struct {
	idx   int
	score float64
}

// rankIndices returns, for each element, its 0-based rank by descending score
// (rank 0 = highest score). Ties share consecutive ranks by input order.
func rankIndices(scores []float64) []int {
	order := make([]int, len(scores))
	for i := range order {
		order[i] = i
	}
	sort.SliceStable(order, func(a, b int) bool { return scores[order[a]] > scores[order[b]] })
	rank := make([]int, len(scores))
	for r, idx := range order {
		rank[idx] = r
	}
	return rank
}

// keywordScore is a BM25-lite term-overlap score with a title boost.
func keywordScore(qTerms []string, m model.Memory) float64 {
	if len(qTerms) == 0 {
		return 0
	}
	title := tokenizeSet(m.Title)
	body := tokenizeSet(m.Content)
	tagSet := map[string]bool{}
	for _, t := range m.Tags {
		tagSet[strings.ToLower(t)] = true
	}
	docLen := float64(len(body) + len(title))
	if docLen == 0 {
		docLen = 1
	}
	var score float64
	for _, term := range qTerms {
		if body[term] {
			score += 1.0
		}
		if title[term] {
			score += 1.5 // title matches weigh more
		}
		if tagSet[term] {
			score += 2.0 // exact tag match is a strong signal
		}
	}
	// length normalization (favor concise, on-topic memories)
	return score / (1.0 + 0.0015*docLen)
}

func tokenizeSet(s string) map[string]bool {
	set := map[string]bool{}
	for _, t := range tokenize(s) {
		set[t] = true
	}
	return set
}

// tokenize is shared with the embed package's scheme (lowercase, alnum splits).
func tokenize(s string) []string {
	return strings.FieldsFunc(strings.ToLower(s), func(r rune) bool {
		return !isAlnum(r)
	})
}

func isAlnum(r rune) bool {
	return (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9')
}

// filterByTags keeps rows that carry at least one of the requested tags.
func filterByTags(rows []store.SearchRow, tags []string) []store.SearchRow {
	if len(tags) == 0 {
		return rows
	}
	want := map[string]bool{}
	for _, t := range tags {
		want[strings.ToLower(strings.TrimSpace(t))] = true
	}
	var out []store.SearchRow
	for _, r := range rows {
		for _, t := range r.Memory.Tags {
			if want[strings.ToLower(t)] {
				out = append(out, r)
				break
			}
		}
	}
	return out
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
