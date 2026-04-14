package jobs

import (
	"context"
	"crypto/md5"
	"encoding/hex"
	"fmt"
	"log/slog"
	"sort"
	"time"

	"github.com/gocraft/dbr/v2"

	"github.com/Crawbl-AI/crawbl-backend/internal/orchestrator/memory"
	"github.com/Crawbl-AI/crawbl-backend/internal/pkg/database"
)

const (
	// centroidSampleWindowDays is the lookback for LLM-labelled drawers
	// when training a centroid. 90 days balances drift response against
	// sample size in a busy workspace.
	centroidSampleWindowDays = 90
	// centroidTopN caps the sample cohort per memory type so one noisy
	// type cannot dominate compute and one quiet type cannot be drowned.
	centroidTopN = 500
	// centroidMemoryTypeHint is the initial capacity hint for the
	// per-type grouping map. Matches the count of declared memory
	// types in memory/types.go (decision, preference, milestone,
	// problem, emotional, fact, task) so the map never has to grow.
	centroidMemoryTypeHint = 7
)

// CentroidRecomputeDeps holds dependencies for the centroid recompute job.
// Repo fields reference consumer-side interfaces declared in ports.go so
// the job layer never imports producer-owned interfaces.
type CentroidRecomputeDeps struct {
	DB           *dbr.Connection
	DrawerRepo   drawerStore
	CentroidRepo centroidStore
	Logger       *slog.Logger
}

// CentroidRecomputeResult is the summary line for one centroid sweep.
type CentroidRecomputeResult struct {
	Updated   int
	Unchanged int
	Skipped   int
}

// RunCentroidRecompute pulls the last 90 days of LLM-labelled drawers
// (capped at 500 per memory type) via the drawer repo, groups by type,
// averages the embeddings in Go, and upserts the resulting centroid
// rows. Types with fewer than memory.MemoryCentroidMinSamples samples
// are skipped so a cold-start workspace cannot be dominated by a
// low-cohort centroid.
func RunCentroidRecompute(ctx context.Context, deps CentroidRecomputeDeps) (*CentroidRecomputeResult, error) {
	if deps.CentroidRepo == nil || deps.DrawerRepo == nil {
		return &CentroidRecomputeResult{}, nil
	}
	sess := deps.DB.NewSession(nil)

	samples, err := deps.DrawerRepo.ListCentroidTrainingSamples(ctx, sess, centroidSampleWindowDays, centroidTopN)
	if err != nil {
		return nil, fmt.Errorf("load centroid samples: %w", err)
	}
	if len(samples) == 0 {
		return &CentroidRecomputeResult{}, nil
	}

	grouped := groupCentroidSamples(samples)
	existingHash, err := loadExistingCentroidHashes(ctx, sess, deps.CentroidRepo)
	if err != nil {
		return nil, err
	}

	toUpsert, result := buildCentroidUpserts(grouped, existingHash)
	if len(toUpsert) == 0 {
		return result, nil
	}
	if err := deps.CentroidRepo.Upsert(ctx, sess, toUpsert); err != nil {
		return nil, fmt.Errorf("upsert centroids: %w", err)
	}
	result.Updated = len(toUpsert)
	return result, nil
}

// loadExistingCentroidHashes indexes the current centroid source hashes
// by memory type so the recompute can no-op types whose sample cohort
// hasn't changed since the last sweep.
func loadExistingCentroidHashes(ctx context.Context, sess database.SessionRunner, repo centroidStore) (map[string]string, error) {
	existing, err := repo.GetAll(ctx, sess)
	if err != nil {
		return nil, fmt.Errorf("load existing centroids: %w", err)
	}
	out := make(map[string]string, len(existing))
	for i := range existing {
		out[existing[i].MemoryType] = existing[i].SourceHash
	}
	return out, nil
}

// buildCentroidUpserts iterates memory types in sorted order so upsert
// sequence, log ordering, and partial-failure recovery are deterministic
// across runs (Go's map iteration is randomised). Returns the upsert
// batch plus a running result tally.
func buildCentroidUpserts(grouped map[string][]memory.CentroidTrainingSample, existingHash map[string]string) ([]memory.MemoryTypeCentroid, *CentroidRecomputeResult) {
	memTypes := make([]string, 0, len(grouped))
	for memType := range grouped {
		memTypes = append(memTypes, memType)
	}
	sort.Strings(memTypes)

	result := &CentroidRecomputeResult{}
	toUpsert := make([]memory.MemoryTypeCentroid, 0, len(grouped))
	for _, memType := range memTypes {
		centroid, ok := buildCentroidForType(memType, grouped[memType], existingHash[memType], result)
		if ok {
			toUpsert = append(toUpsert, centroid)
		}
	}
	return toUpsert, result
}

// buildCentroidForType prepares one centroid row for upsert. Tallies the
// skipped/unchanged counters on result and returns (row, true) only for
// the types that must be persisted.
func buildCentroidForType(memType string, rows []memory.CentroidTrainingSample, existingHash string, result *CentroidRecomputeResult) (memory.MemoryTypeCentroid, bool) {
	if len(rows) < memory.MemoryCentroidMinSamples {
		result.Skipped++
		return memory.MemoryTypeCentroid{}, false
	}
	centroid := averageVectors(rows)
	if centroid == nil {
		result.Skipped++
		return memory.MemoryTypeCentroid{}, false
	}
	hash := hashSampleIDs(rows)
	if existingHash == hash {
		result.Unchanged++
		return memory.MemoryTypeCentroid{}, false
	}
	return memory.MemoryTypeCentroid{
		MemoryType:  memType,
		Centroid:    centroid,
		SampleCount: len(rows),
		ComputedAt:  time.Now().UTC(),
		SourceHash:  hash,
	}, true
}

// groupCentroidSamples buckets samples by memory type so callers can
// iterate types independently.
func groupCentroidSamples(samples []memory.CentroidTrainingSample) map[string][]memory.CentroidTrainingSample {
	out := make(map[string][]memory.CentroidTrainingSample, centroidMemoryTypeHint)
	for i := range samples {
		t := samples[i].MemoryType
		out[t] = append(out[t], samples[i])
	}
	return out
}

// averageVectors computes the element-wise average of the embeddings
// in rows. Returns nil when rows is empty or its vectors have no
// consistent dimension — defensive, should never happen in practice.
func averageVectors(rows []memory.CentroidTrainingSample) []float32 {
	if len(rows) == 0 {
		return nil
	}
	dim := len(rows[0].Embedding)
	if dim == 0 {
		return nil
	}
	sum := make([]float64, dim)
	for i := range rows {
		vec := rows[i].Embedding
		if len(vec) != dim {
			return nil
		}
		for j, v := range vec {
			sum[j] += float64(v)
		}
	}
	out := make([]float32, dim)
	n := float64(len(rows))
	for i, v := range sum {
		out[i] = float32(v / n)
	}
	return out
}

// hashSampleIDs produces a deterministic hash of the sample ID list so
// the upsert can no-op when the cohort has not changed since the last
// run. IDs are sorted to stabilise the hash.
func hashSampleIDs(rows []memory.CentroidTrainingSample) string {
	ids := make([]string, len(rows))
	for i := range rows {
		ids[i] = rows[i].ID
	}
	sort.Strings(ids)
	h := md5.New()
	for _, id := range ids {
		_, _ = h.Write([]byte(id))
		_, _ = h.Write([]byte{0})
	}
	return hex.EncodeToString(h.Sum(nil))
}
