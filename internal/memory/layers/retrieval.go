package layers

import (
	"context"
	"math"
	"strings"
	"sync"
	"time"

	"github.com/Crawbl-AI/crawbl-backend/internal/memory"
	drawerpkg "github.com/Crawbl-AI/crawbl-backend/internal/memory/drawer"
	"github.com/Crawbl-AI/crawbl-backend/internal/memory/kg"
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
)

// HybridRetrieve performs parallel pgvector search + KG entity lookup,
// merges results, and ranks by importance × recency × relevance + agent affinity.
func HybridRetrieve(
	ctx context.Context,
	sess database.SessionRunner,
	drawerRepo drawerpkg.Repo,
	kgGraph kg.Graph,
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

	var (
		vectorResults []memory.DrawerSearchResult
		kgDrawerIDs   []string
		vectorErr     error
		wg            sync.WaitGroup
	)

	// Parallel fan-out: vector search + KG entity lookup.
	wg.Add(1)
	go func() {
		defer wg.Done()
		vectorResults, vectorErr = drawerRepo.Search(ctx, sess, workspaceID, queryEmbedding, "", "", limit*2)
	}()

	// KG lookup: two-step (exact match first, embedding fallback).
	if kgGraph != nil {
		wg.Add(1)
		go func() {
			defer wg.Done()
			kgDrawerIDs = kgEntityLookup(ctx, sess, kgGraph, workspaceID, query)
		}()
	}

	wg.Wait()

	if vectorErr != nil {
		return nil, vectorErr
	}

	// Merge results by drawer ID.
	seen := make(map[string]*RetrievalResult)

	for i := range vectorResults {
		r := &vectorResults[i]
		seen[r.ID] = &RetrievalResult{
			Drawer:     r.Drawer,
			Similarity: r.Similarity,
		}
	}

	// KG results: boost existing vector results and load KG-only drawers.
	for _, id := range kgDrawerIDs {
		if existing, exists := seen[id]; exists {
			existing.GraphScore = 1.0
			continue
		}
		// Load drawer from DB for KG-only hits.
		d, err := drawerRepo.GetByID(ctx, sess, workspaceID, id)
		if err != nil || d == nil {
			continue
		}
		seen[id] = &RetrievalResult{
			Drawer:     *d,
			GraphScore: 1.0,
		}
	}

	// Rank and collect.
	results := make([]RetrievalResult, 0, len(seen))
	now := time.Now()

	for _, r := range seen {
		relevance := math.Max(r.Similarity, r.GraphScore)
		recency := recencyFactor(r.LastAccessedAt, now)
		affinity := 0.0
		if agentSlug != "" && r.AddedByAgent == agentSlug {
			affinity = agentAffinityBoost
		}
		r.FinalScore = r.Importance*recency*relevance + affinity
		results = append(results, *r)
	}

	// Sort by FinalScore descending.
	sortByScore(results)

	// Touch access for returned results.
	for i := range results {
		if i >= limit {
			break
		}
		_ = drawerRepo.TouchAccess(ctx, sess, results[i].ID)
	}

	if len(results) > limit {
		results = results[:limit]
	}

	return results, nil
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

// sortByScore sorts results by FinalScore descending.
func sortByScore(results []RetrievalResult) {
	for i := 1; i < len(results); i++ {
		for j := i; j > 0 && results[j].FinalScore > results[j-1].FinalScore; j-- {
			results[j], results[j-1] = results[j-1], results[j]
		}
	}
}

// kgEntityLookup performs two-step entity lookup:
// Step 1: exact match on entity names extracted from query (~1ms)
// Step 2: embedding fallback (if Step 1 returns nothing) — deferred to backlog
func kgEntityLookup(ctx context.Context, sess database.SessionRunner, kgGraph kg.Graph, workspaceID, query string) []string {
	// Step 1: Extract words > 3 chars, try exact match.
	words := extractQueryWords(query)
	if len(words) == 0 {
		return nil
	}

	var drawerIDs []string
	for _, word := range words {
		// Query KG for this entity.
		results, err := kgGraph.QueryEntity(ctx, sess, workspaceID, word, "", "both")
		if err != nil || len(results) == 0 {
			continue
		}
		// Collect source_closet references (which contain drawer IDs).
		for i := range results {
			r := &results[i]
			if r.SourceCloset != "" {
				drawerIDs = append(drawerIDs, r.SourceCloset)
			}
		}
	}

	return drawerIDs
}

// extractQueryWords returns lowercase words longer than 3 characters.
func extractQueryWords(query string) []string {
	words := strings.Fields(strings.ToLower(query))
	var filtered []string
	for _, w := range words {
		// Strip punctuation.
		w = strings.Trim(w, ".,;:!?\"'()-")
		if len(w) > 3 {
			filtered = append(filtered, w)
		}
	}
	return filtered
}
