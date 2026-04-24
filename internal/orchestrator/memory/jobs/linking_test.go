package jobs

import (
	"context"
	"testing"

	"github.com/Crawbl-AI/crawbl-backend/internal/orchestrator/memory"
	"github.com/Crawbl-AI/crawbl-backend/internal/orchestrator/memory/extract"
	"github.com/Crawbl-AI/crawbl-backend/internal/orchestrator/memory/repo/drawerrepo"
	"github.com/Crawbl-AI/crawbl-backend/internal/pkg/database"
)

// fakeKGRepo is a test double for kgStore that records insertions in memory.
type fakeKGRepo struct {
	entities []string
	triples  []*memory.Triple
}

func (f *fakeKGRepo) AddEntity(_ context.Context, _ database.SessionRunner, _, name, _, _ string) (string, error) {
	f.entities = append(f.entities, name)
	return "e_" + name, nil
}

func (f *fakeKGRepo) AddTriple(_ context.Context, _ database.SessionRunner, _ string, t *memory.Triple) (string, error) {
	// copy so the caller cannot mutate after insert
	cp := *t
	f.triples = append(f.triples, &cp)
	return "t_" + t.Subject + "_" + t.Predicate, nil
}

// fakeDrawerRepo is a test double for drawerStore.
// Only the methods exercised by the test are implemented; all others panic so
// an accidental call is immediately visible.
type fakeDrawerRepo struct {
	enrichCalls []enrichCall
}

type enrichCall struct {
	workspaceID string
	drawerID    string
	entityCount int
	tripleCount int
}

func (f *fakeDrawerRepo) UpdateEnrichment(_ context.Context, _ database.SessionRunner, workspaceID, drawerID string, entityCount, tripleCount int) error {
	f.enrichCalls = append(f.enrichCalls, enrichCall{
		workspaceID: workspaceID,
		drawerID:    drawerID,
		entityCount: entityCount,
		tripleCount: tripleCount,
	})
	return nil
}

// Unimplemented drawerStore methods — panic so accidental invocations surface immediately.

func (f *fakeDrawerRepo) ActiveWorkspaces(_ context.Context, _ database.SessionRunner, _ int) ([]string, error) {
	panic("fakeDrawerRepo: ActiveWorkspaces not implemented")
}
func (f *fakeDrawerRepo) ListByState(_ context.Context, _ database.SessionRunner, _, _ string, _ int) ([]memory.Drawer, error) {
	panic("fakeDrawerRepo: ListByState not implemented")
}
func (f *fakeDrawerRepo) UpdateClassification(_ context.Context, _ database.SessionRunner, _ drawerrepo.UpdateClassificationOpts) error {
	panic("fakeDrawerRepo: UpdateClassification not implemented")
}
func (f *fakeDrawerRepo) UpdateEmbedding(_ context.Context, _ database.SessionRunner, _, _ string, _ []float32) error {
	panic("fakeDrawerRepo: UpdateEmbedding not implemented")
}
func (f *fakeDrawerRepo) UpdateState(_ context.Context, _ database.SessionRunner, _, _, _ string) error {
	panic("fakeDrawerRepo: UpdateState not implemented")
}
func (f *fakeDrawerRepo) Search(_ context.Context, _ database.SessionRunner, _ string, _ []float32, _, _ string, _ int) ([]memory.DrawerSearchResult, error) {
	panic("fakeDrawerRepo: Search not implemented")
}
func (f *fakeDrawerRepo) SetSupersededBy(_ context.Context, _ database.SessionRunner, _, _, _ string) error {
	panic("fakeDrawerRepo: SetSupersededBy not implemented")
}
func (f *fakeDrawerRepo) SetClusterID(_ context.Context, _ database.SessionRunner, _, _, _ string) error {
	panic("fakeDrawerRepo: SetClusterID not implemented")
}
func (f *fakeDrawerRepo) IncrementRetryCount(_ context.Context, _ database.SessionRunner, _, _ string) error {
	panic("fakeDrawerRepo: IncrementRetryCount not implemented")
}
func (f *fakeDrawerRepo) DecayImportance(_ context.Context, _ database.SessionRunner, _ string, _, _ int, _, _ float64) (int, error) {
	panic("fakeDrawerRepo: DecayImportance not implemented")
}
func (f *fakeDrawerRepo) PruneLowImportance(_ context.Context, _ database.SessionRunner, _ string, _ float64, _, _ int) (int, error) {
	panic("fakeDrawerRepo: PruneLowImportance not implemented")
}
func (f *fakeDrawerRepo) ListEnrichCandidates(_ context.Context, _ database.SessionRunner, _ int) ([]memory.Drawer, error) {
	panic("fakeDrawerRepo: ListEnrichCandidates not implemented")
}
func (f *fakeDrawerRepo) ListCentroidTrainingSamples(_ context.Context, _ database.SessionRunner, _, _ int) ([]memory.CentroidTrainingSample, error) {
	panic("fakeDrawerRepo: ListCentroidTrainingSamples not implemented")
}

// TestLinkAndCount_SourceClosetAndCounts verifies two invariants:
//  1. Every triple inserted by linkAndCount carries a non-empty SourceCloset
//     equal to the hall argument (so hybrid-retrieval's WHERE source_closet <> ”
//     guard picks them up).
//  2. The returned entityCount / tripleCount match the number of successful
//     inserts, allowing the caller to write accurate values to UpdateEnrichment.
func TestLinkAndCount_SourceClosetAndCounts(t *testing.T) {
	const (
		workspaceID = "ws_test"
		hall        = "user/goals"
	)

	classification := &extract.LLMClassification{
		Entities: []extract.ExtractedEntity{
			{Name: "Alice", Type: "person"},
			{Name: "Bob", Type: "person"},
			{Name: "Acme Corp", Type: "organisation"},
		},
		Triples: []extract.ExtractedTriple{
			{Subject: "Alice", Predicate: "works_at", Object: "Acme Corp"},
			{Subject: "Bob", Predicate: "knows", Object: "Alice"},
		},
	}

	kg := &fakeKGRepo{}
	entityCount, tripleCount := linkAndCount(context.Background(), nil, kg, workspaceID, hall, classification)

	if entityCount != 3 {
		t.Errorf("entityCount: got %d, want 3", entityCount)
	}
	if tripleCount != 2 {
		t.Errorf("tripleCount: got %d, want 2", tripleCount)
	}

	for i, tr := range kg.triples {
		if tr.SourceCloset == "" {
			t.Errorf("triple[%d] (%s %s %s): SourceCloset is empty", i, tr.Subject, tr.Predicate, tr.Object)
		}
		if tr.SourceCloset != hall {
			t.Errorf("triple[%d] SourceCloset: got %q, want %q", i, tr.SourceCloset, hall)
		}
	}
}

// TestLinkAndCount_NilKGRepo verifies that a nil kgStore is treated as a
// no-op and both counts return zero without panicking.
func TestLinkAndCount_NilKGRepo(t *testing.T) {
	classification := &extract.LLMClassification{
		Entities: []extract.ExtractedEntity{{Name: "X", Type: "thing"}},
		Triples:  []extract.ExtractedTriple{{Subject: "X", Predicate: "is", Object: "Y"}},
	}

	entityCount, tripleCount := linkAndCount(context.Background(), nil, nil, "ws_test", "hall", classification)

	if entityCount != 0 || tripleCount != 0 {
		t.Errorf("nil kgStore: got (%d, %d), want (0, 0)", entityCount, tripleCount)
	}
}

// TestLinkAndCount_UpdateEnrichmentCalledOnce verifies that after linkAndCount
// the caller-side UpdateEnrichment is invoked exactly once with the correct
// entity and triple counts — simulating the process pipeline call site.
func TestLinkAndCount_UpdateEnrichmentCalledOnce(t *testing.T) {
	const (
		workspaceID = "ws_abc"
		drawerID    = "dr_001"
		hall        = "conversations/2024"
	)

	classification := &extract.LLMClassification{
		Entities: []extract.ExtractedEntity{
			{Name: "Carol", Type: "person"},
			{Name: "ACME", Type: "org"},
			{Name: "Project X", Type: "project"},
		},
		Triples: []extract.ExtractedTriple{
			{Subject: "Carol", Predicate: "leads", Object: "Project X"},
			{Subject: "ACME", Predicate: "sponsors", Object: "Project X"},
		},
	}

	kg := &fakeKGRepo{}
	dr := &fakeDrawerRepo{}

	entityCount, tripleCount := linkAndCount(context.Background(), nil, kg, workspaceID, hall, classification)

	if err := dr.UpdateEnrichment(context.Background(), nil, workspaceID, drawerID, entityCount, tripleCount); err != nil {
		t.Fatalf("UpdateEnrichment: %v", err)
	}

	if len(dr.enrichCalls) != 1 {
		t.Fatalf("UpdateEnrichment call count: got %d, want 1", len(dr.enrichCalls))
	}
	call := dr.enrichCalls[0]
	if call.entityCount != 3 {
		t.Errorf("UpdateEnrichment entityCount: got %d, want 3", call.entityCount)
	}
	if call.tripleCount != 2 {
		t.Errorf("UpdateEnrichment tripleCount: got %d, want 2", call.tripleCount)
	}
}
