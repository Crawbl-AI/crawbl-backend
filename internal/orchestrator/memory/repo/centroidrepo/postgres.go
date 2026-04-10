// Package centroidrepo provides the PostgreSQL implementation of the
// MemPalace centroid repository. Centroids are seven prototype vectors
// (one per memory type) used by the Phase 2 nearest-centroid classifier
// in the autoingest worker. A weekly recompute job keeps them aligned
// with recent LLM-labelled drawers.
package centroidrepo

import (
	"context"
	"errors"
	"fmt"

	"github.com/gocraft/dbr/v2"
	"github.com/pgvector/pgvector-go"

	"github.com/Crawbl-AI/crawbl-backend/internal/orchestrator/memory"
	"github.com/Crawbl-AI/crawbl-backend/internal/pkg/database"
)

// Postgres is the memory_type_centroids repository backed by
// PostgreSQL with pgvector. It implements repo.CentroidRepo.
type Postgres struct{}

// NewPostgres creates a new centroid repository.
func NewPostgres() *Postgres {
	return &Postgres{}
}

// GetAll returns every centroid row regardless of sample_count.
func (r *Postgres) GetAll(ctx context.Context, sess database.SessionRunner) ([]memory.MemoryTypeCentroid, error) {
	type row struct {
		MemoryType  string          `db:"memory_type"`
		Centroid    pgvector.Vector `db:"centroid"`
		SampleCount int             `db:"sample_count"`
		ComputedAt  dbr.NullTime    `db:"computed_at"`
		SourceHash  string          `db:"source_hash"`
	}
	var rows []row
	_, err := sess.SelectBySql(
		`SELECT memory_type, centroid, sample_count, computed_at, source_hash
		 FROM memory_type_centroids`,
	).LoadContext(ctx, &rows)
	if err != nil {
		return nil, fmt.Errorf("centroid: get all: %w", err)
	}
	out := make([]memory.MemoryTypeCentroid, 0, len(rows))
	for i := range rows {
		c := memory.MemoryTypeCentroid{
			MemoryType:  rows[i].MemoryType,
			Centroid:    rows[i].Centroid.Slice(),
			SampleCount: rows[i].SampleCount,
			SourceHash:  rows[i].SourceHash,
		}
		if rows[i].ComputedAt.Valid {
			c.ComputedAt = rows[i].ComputedAt.Time
		}
		out = append(out, c)
	}
	return out, nil
}

// Upsert writes a batch of centroids, skipping rows whose source_hash
// is unchanged from the existing value. Each row is sent as a separate
// statement because pgvector parameter packing for bulk insert is not
// worth the extra complexity at the 7-row ceiling.
func (r *Postgres) Upsert(ctx context.Context, sess database.SessionRunner, rows []memory.MemoryTypeCentroid) error {
	for i := range rows {
		c := &rows[i]
		if len(c.Centroid) == 0 {
			continue
		}
		vec := pgvector.NewVector(c.Centroid)
		_, err := sess.InsertBySql(
			`INSERT INTO memory_type_centroids (memory_type, centroid, sample_count, computed_at, source_hash)
			 VALUES (?, ?, ?, NOW(), ?)
			 ON CONFLICT (memory_type) DO UPDATE SET
			   centroid     = EXCLUDED.centroid,
			   sample_count = EXCLUDED.sample_count,
			   computed_at  = EXCLUDED.computed_at,
			   source_hash  = EXCLUDED.source_hash
			 WHERE memory_type_centroids.source_hash IS DISTINCT FROM EXCLUDED.source_hash`,
			c.MemoryType, vec, c.SampleCount, c.SourceHash,
		).ExecContext(ctx)
		if err != nil {
			return fmt.Errorf("centroid: upsert %q: %w", c.MemoryType, err)
		}
	}
	return nil
}

// NearestType returns the closest memory-type centroid to the given
// embedding. Honors MemoryCentroidMinSamples so a cold-start workspace
// cannot be dominated by a low-sample-count centroid. workspaceID is
// unused today (centroids are global) but kept on the interface so
// future per-workspace personalisation can land without an API change.
func (r *Postgres) NearestType(ctx context.Context, sess database.SessionRunner, _ string, embedding []float32) (string, float64, bool, error) {
	if len(embedding) == 0 {
		return "", 0, false, nil
	}
	type row struct {
		MemoryType string  `db:"memory_type"`
		Similarity float64 `db:"similarity"`
	}
	vec := pgvector.NewVector(embedding)
	var picked row
	err := sess.SelectBySql(
		`SELECT memory_type, 1 - (centroid <=> $1) AS similarity
		 FROM memory_type_centroids
		 WHERE sample_count >= $2
		 ORDER BY centroid <=> $1
		 LIMIT 1`,
		vec, memory.MemoryCentroidMinSamples,
	).LoadOneContext(ctx, &picked)
	if err != nil {
		if errors.Is(err, dbr.ErrNotFound) {
			return "", 0, false, nil
		}
		return "", 0, false, fmt.Errorf("centroid: nearest type: %w", err)
	}
	return picked.MemoryType, picked.Similarity, true, nil
}
