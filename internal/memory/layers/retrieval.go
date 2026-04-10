package layers

import (
	"context"
	"log/slog"
	"math"
	"sort"
	"strings"
	"time"

	"github.com/Crawbl-AI/crawbl-backend/internal/memory"
	memrepo "github.com/Crawbl-AI/crawbl-backend/internal/memory/repo"
	"github.com/Crawbl-AI/crawbl-backend/internal/pkg/database"
	"github.com/Crawbl-AI/crawbl-backend/internal/pkg/embed"
)

// RetrievalResult extends a drawer with ranking scores.
type RetrievalResult struct {
	memory.Drawer
	Similarity float64
	GraphScore float64
	FinalScore float64
}

const (
	// agentAffinityBoost is added when the requesting agent matches the drawer's creator.
	agentAffinityBoost   = 0.1
	defaultRecencyFactor = 0.5
	hoursPerDay          = 24.0
	// minQueryWordLen is the minimum length for a query token to be forwarded
	// as a KG lookup term. Shorter words are noise (stopwords, articles).
	minQueryWordLen = 4
)

// HybridRetrieve issues a single hybrid search query (pgvector ANN unioned
// with a KG entity-name lookup) and ranks the merged results by
// importance × recency × relevance + agent affinity.
//
// This function does no fan-out — the CTE in drawerrepo.SearchHybrid does
// both lookups in one round-trip, so no goroutines are involved.
func HybridRetrieve(
	ctx context.Context,
	sess database.SessionRunner,
	drawerRepo memrepo.DrawerRepo,
	embedder embed.Embedder,
	workspaceID, query, agentSlug string,
	limit int,
) ([]RetrievalResult, error) {
	if limit <= 0 {
		limit = 5
	}

	queryEmbedding, err := embedder.Embed(ctx, query)
	if err != nil {
		return nil, err
	}

	terms := extractQueryWords(query)

	rows, err := drawerRepo.SearchHybrid(ctx, sess, workspaceID, queryEmbedding, terms, limit*2)
	if err != nil {
		return nil, err
	}

	results := rankHybridResults(rows, agentSlug, time.Now())

	if len(results) > limit {
		results = results[:limit]
	}

	touchReturnedDrawers(ctx, sess, drawerRepo, workspaceID, results)
	return results, nil
}

// touchReturnedDrawers bumps last_accessed_at on every drawer we just
// surfaced so recency scoring reflects the interaction. Errors are logged
// and swallowed — a failed touch never blocks a user-facing search.
func touchReturnedDrawers(ctx context.Context, sess database.SessionRunner, drawerRepo memrepo.DrawerRepo, workspaceID string, results []RetrievalResult) {
	for i := range results {
		if err := drawerRepo.TouchAccess(ctx, sess, workspaceID, results[i].ID); err != nil {
			slog.WarnContext(ctx, "memory-retrieval: touch access failed",
				slog.String("drawer_id", results[i].ID),
				slog.String("workspace_id", workspaceID),
				slog.String("error", err.Error()),
			)
		}
	}
}

// rankHybridResults is the pure ranking function: given raw hybrid rows and
// the requesting agent, produce sorted RetrievalResults. No DB, no context,
// no side effects — trivially unit-testable.
func rankHybridResults(rows []memory.HybridSearchResult, agentSlug string, now time.Time) []RetrievalResult {
	results := make([]RetrievalResult, 0, len(rows))
	for i := range rows {
		row := &rows[i]

		graphScore := 0.0
		if row.ViaKG {
			graphScore = 1.0
		}
		relevance := math.Max(row.Similarity, graphScore)
		recency := recencyFactor(row.LastAccessedAt, now)

		affinity := 0.0
		if agentSlug != "" && row.AddedByAgent == agentSlug {
			affinity = agentAffinityBoost
		}

		results = append(results, RetrievalResult{
			Drawer:     row.Drawer,
			Similarity: row.Similarity,
			GraphScore: graphScore,
			FinalScore: row.Importance*recency*relevance + affinity,
		})
	}

	sort.SliceStable(results, func(i, j int) bool {
		return results[i].FinalScore > results[j].FinalScore
	})

	return results
}

// recencyFactor returns a decay factor based on days since last access.
// Formula: 1.0 / (1.0 + daysSinceAccess/30.0)
func recencyFactor(lastAccessed *time.Time, now time.Time) float64 {
	if lastAccessed == nil {
		return defaultRecencyFactor // default for never-accessed drawers
	}
	days := now.Sub(*lastAccessed).Hours() / hoursPerDay
	if days < 0 {
		days = 0
	}
	return 1.0 / (1.0 + days/30.0)
}

// extractQueryWords returns lowercase words of length > minQueryWordLen-1,
// stripped of surrounding punctuation. These feed the KG branch of the
// hybrid search CTE.
func extractQueryWords(query string) []string {
	words := strings.Fields(strings.ToLower(query))
	filtered := make([]string, 0, len(words))
	for _, w := range words {
		w = strings.Trim(w, ".,;:!?\"'()-")
		if len(w) >= minQueryWordLen {
			filtered = append(filtered, w)
		}
	}
	return filtered
}
