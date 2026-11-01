package service

import (
	"context"
	"sort"
	"strings"

	"clovergo/internal/domain"
)

const (
	// rrfK is the reciprocal-rank-fusion constant; 60 is the usual default.
	rrfK = 60
	// retrievalDepth is how many candidates each retriever contributes.
	retrievalDepth = 50
	// A semantic hit must score at least semanticFloor and be within
	// semanticMargin of the best hit. Measured on doubao-embedding-vision-251215
	// at 1024 dimensions: unrelated queries peak around 0.27-0.33 while related
	// items score 0.45-0.65 but sit only ~0.05 apart, and English queries raise
	// every score by ~0.15, so a fixed cut alone either admits noise or drops
	// the answer. Keyword hits are never filtered by these.
	semanticFloor  = 0.40
	semanticMargin = 0.06

	runPurposeQueryEmbedding = "query_embedding"
)

type retrievalResult struct {
	Items        []domain.KnowledgeItem
	Mode         string
	KeywordHits  int
	SemanticHits int
	// DocumentHits is how many materials matched the query semantically. Their
	// knowledge is lifted into the fusion, but no material itself is returned.
	DocumentHits int
	Usage        domain.TokenUsage
}

// retrieve finds current knowledge for a query across the single library. It
// always runs keyword search; when an embedding model is configured the query
// is embedded too and both rankings are merged with reciprocal rank fusion. A
// configured embedder that fails is an error: retrieval never silently drops
// back to keywords.
//
// A non-empty scope restricts both retrievers to one shelf. Keyword hits are
// filtered in SQL; semantic hits are filtered before ranking so the fusion
// sees ranks within the shelf rather than ranks in the whole library with
// holes punched in them.
func (s *Service) retrieve(ctx context.Context, query string, limit int, scope KnowledgeScope) (retrievalResult, error) {
	query = strings.TrimSpace(query)
	result := retrievalResult{Mode: "keyword"}
	var scopeIDs map[int64]struct{}
	if !scope.empty() {
		ids, err := s.repo.KnowledgeIDsInScope(ctx, scope.filter())
		if err != nil {
			return result, appError(CodeInternal, "read knowledge scope failed", err)
		}
		scopeIDs = make(map[int64]struct{}, len(ids))
		for _, id := range ids {
			scopeIDs[id] = struct{}{}
		}
	}
	keywordIDs, err := s.repo.SearchKnowledgeIDs(ctx, query, retrievalDepth, scope.filter())
	if err != nil {
		return result, appError(CodeInternal, "keyword search failed", err)
	}
	result.KeywordHits = len(keywordIDs)
	scores := make(map[int64]float64)
	matches := make(map[int64]string)
	for rank, id := range keywordIDs {
		scores[id] += 1.0 / float64(rrfK+rank+1)
		matches[id] = "keyword"
	}
	similarity := make(map[int64]float64)
	if s.embedder.Configured() && query != "" {
		result.Mode = "hybrid"
		queryVector, usage, err := s.embedder.Embed(ctx, query)
		result.Usage = usage
		if err != nil {
			return result, mapArkError(err, "Ark embedding request failed", "Ark returned an invalid embedding")
		}
		vectors, err := s.repo.CurrentVectors(ctx, s.embedder.Model(), s.embedder.Dimensions())
		if err != nil {
			return result, appError(CodeInternal, "read knowledge vectors failed", err)
		}
		candidates := make([]semanticCandidate, 0, len(vectors))
		for _, vector := range vectors {
			if scopeIDs != nil {
				if _, ok := scopeIDs[vector.KnowledgeID]; !ok {
					continue
				}
			}
			candidates = append(candidates, semanticCandidate{id: vector.KnowledgeID, vector: vector.Vector})
		}
		hits := topSemanticHits(queryVector, candidates, retrievalDepth)
		result.SemanticHits = len(hits)
		for rank, hit := range hits {
			scores[hit.id] += 1.0 / float64(rrfK+rank+1)
			similarity[hit.id] = hit.score
			markMatch(matches, hit.id)
		}

		// Materials carry their own vector, taken over the summary the model
		// wrote at upload. A material the query matches does not have to be
		// returned itself: what a caller can quote is the knowledge extracted
		// from it, so a material hit lifts the knowledge it produced into the
		// same fusion. Knowledge outside the requested scope is never lifted.
		documentVectors, err := s.repo.CurrentDocumentVectors(ctx, s.embedder.Model(), s.embedder.Dimensions())
		if err != nil {
			return result, appError(CodeInternal, "read material vectors failed", err)
		}
		documentCandidates := make([]semanticCandidate, 0, len(documentVectors))
		for _, vector := range documentVectors {
			documentCandidates = append(documentCandidates, semanticCandidate{id: vector.DocumentID, vector: vector.Vector})
		}
		documentHits := topSemanticHits(queryVector, documentCandidates, retrievalDepth)
		result.DocumentHits = len(documentHits)
		if len(documentHits) > 0 {
			documentIDs := make([]int64, 0, len(documentHits))
			for _, hit := range documentHits {
				documentIDs = append(documentIDs, hit.id)
			}
			byDocument, err := s.repo.KnowledgeIDsByDocumentIDs(ctx, documentIDs)
			if err != nil {
				return result, appError(CodeInternal, "read knowledge by material failed", err)
			}
			for rank, hit := range documentHits {
				for _, knowledgeID := range byDocument[hit.id] {
					if scopeIDs != nil {
						if _, ok := scopeIDs[knowledgeID]; !ok {
							continue
						}
					}
					scores[knowledgeID] += 1.0 / float64(rrfK+rank+1)
					if _, ok := similarity[knowledgeID]; !ok {
						similarity[knowledgeID] = hit.score
					}
					markMatch(matches, knowledgeID)
				}
			}
		}
	}
	ids := make([]int64, 0, len(scores))
	for id := range scores {
		ids = append(ids, id)
	}
	sort.Slice(ids, func(i, j int) bool {
		if scores[ids[i]] != scores[ids[j]] {
			return scores[ids[i]] > scores[ids[j]]
		}
		return ids[i] > ids[j]
	})
	if len(ids) == 0 {
		result.Items = []domain.KnowledgeItem{}
		return result, nil
	}
	items, err := s.repo.GetKnowledgeByIDs(ctx, ids)
	if err != nil {
		return result, appError(CodeInternal, "read retrieved knowledge failed", err)
	}
	result.Items = make([]domain.KnowledgeItem, 0, limit)
	for _, item := range items {
		item.Match, item.Score = matches[item.ID], similarity[item.ID]
		result.Items = append(result.Items, item)
		if len(result.Items) == limit {
			break
		}
	}
	return result, nil
}

// semanticCandidate is one stored vector offered to the semantic ranking,
// whether it belongs to a knowledge item or a material.
type semanticCandidate struct {
	id     int64
	vector []float32
}

type semanticHit struct {
	id    int64
	score float64
}

// topSemanticHits scores every candidate against the query vector and keeps the
// strongest ones under the measured floor and margin, up to depth. Knowledge
// and materials are ranked with the same cut so a material hit and an item hit
// mean the same thing to the fusion.
func topSemanticHits(queryVector []float32, candidates []semanticCandidate, depth int) []semanticHit {
	hits := make([]semanticHit, 0, len(candidates))
	for _, candidate := range candidates {
		hits = append(hits, semanticHit{id: candidate.id, score: dot(queryVector, candidate.vector)})
	}
	sort.Slice(hits, func(i, j int) bool { return hits[i].score > hits[j].score })
	kept := 0
	for kept < len(hits) && kept < depth &&
		hits[kept].score >= semanticFloor && hits[kept].score >= hits[0].score-semanticMargin {
		kept++
	}
	return hits[:kept]
}

// markMatch records how one knowledge item was found. A keyword hit that also
// matched semantically becomes "both"; anything else records as "semantic".
func markMatch(matches map[int64]string, id int64) {
	if matches[id] == "keyword" {
		matches[id] = "both"
	} else {
		matches[id] = "semantic"
	}
}

func dot(left, right []float32) float64 {
	if len(left) != len(right) {
		return 0
	}
	var sum float64
	for i := range left {
		sum += float64(left[i]) * float64(right[i])
	}
	return sum
}
