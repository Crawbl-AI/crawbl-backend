package mcp

import (
	"context"
	"crypto/sha256"
	"fmt"
	"strings"
	"time"

	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/Crawbl-AI/crawbl-backend/internal/memory"
)

const (
	classifierMinConfidence = 0.5
	importanceScale         = 5.0
	defaultImportance       = 3.0
)

// --- Input/output types: Read tools ---

type memoryStatusInput struct{}

type memoryStatusOutput struct {
	TotalDrawers int                `json:"total_drawers"`
	Wings        []memory.WingCount `json:"wings"`
	Rooms        []memory.RoomCount `json:"rooms"`
}

type memoryListWingsInput struct{}

type memoryListWingsOutput struct {
	Wings []memory.WingCount `json:"wings"`
}

type memoryListRoomsInput struct {
	Wing string `json:"wing,omitempty" jsonschema:"optional wing filter"`
}

type memoryListRoomsOutput struct {
	Rooms []memory.RoomCount `json:"rooms"`
}

type memoryGetTaxonomyInput struct{}

type memoryGetTaxonomyOutput struct {
	Taxonomy map[string]map[string]int `json:"taxonomy"`
}

type memorySearchInput struct {
	Query string `json:"query" jsonschema:"what to search for"`
	Limit int    `json:"limit,omitempty" jsonschema:"max results (default 5)"`
	Wing  string `json:"wing,omitempty" jsonschema:"optional wing filter"`
	Room  string `json:"room,omitempty" jsonschema:"optional room filter"`
}

type memorySearchOutput struct {
	Results []memorySearchResultBrief `json:"results"`
	Count   int                       `json:"count"`
}

type memorySearchResultBrief struct {
	ID         string  `json:"id"`
	Wing       string  `json:"wing"`
	Room       string  `json:"room"`
	Content    string  `json:"content"`
	MemoryType string  `json:"memory_type"`
	Importance float64 `json:"importance"`
	Similarity float64 `json:"similarity"`
	FiledAt    string  `json:"filed_at"`
}

type memoryCheckDuplicateInput struct {
	Content   string  `json:"content" jsonschema:"content to check for duplicates"`
	Threshold float64 `json:"threshold,omitempty" jsonschema:"similarity threshold (default 0.9)"`
}

type memoryCheckDuplicateOutput struct {
	Duplicates []memorySearchResultBrief `json:"duplicates"`
	HasDupe    bool                      `json:"has_duplicate"`
}

type memoryTraverseInput struct {
	StartRoom string `json:"start_room" jsonschema:"room to start traversal from"`
	MaxHops   int    `json:"max_hops,omitempty" jsonschema:"maximum hops (default 3)"`
}

type memoryTraverseOutput struct {
	Rooms []traversalBrief `json:"rooms"`
	Count int              `json:"count"`
}

type traversalBrief struct {
	Room         string   `json:"room"`
	Wings        []string `json:"wings"`
	Halls        []string `json:"halls"`
	Count        int      `json:"count"`
	Hop          int      `json:"hop"`
	ConnectedVia []string `json:"connected_via,omitempty"`
}

type memoryFindTunnelsInput struct {
	WingA string `json:"wing_a,omitempty" jsonschema:"first wing filter"`
	WingB string `json:"wing_b,omitempty" jsonschema:"second wing filter"`
}

type memoryFindTunnelsOutput struct {
	Tunnels []tunnelBrief `json:"tunnels"`
	Count   int           `json:"count"`
}

type tunnelBrief struct {
	Room  string   `json:"room"`
	Wings []string `json:"wings"`
	Halls []string `json:"halls"`
	Count int      `json:"count"`
}

type memoryGraphStatsInput struct{}

type memoryGraphStatsOutput struct {
	TotalRooms   int            `json:"total_rooms"`
	TunnelRooms  int            `json:"tunnel_rooms"`
	TotalEdges   int            `json:"total_edges"`
	RoomsPerWing map[string]int `json:"rooms_per_wing"`
	TopTunnels   []tunnelBrief  `json:"top_tunnels"`
}

// --- Input/output types: Write tools ---

type memoryAddDrawerInput struct {
	Wing       string `json:"wing" jsonschema:"wing to file the drawer in"`
	Room       string `json:"room" jsonschema:"room within the wing"`
	Content    string `json:"content" jsonschema:"the content to store"`
	SourceFile string `json:"source_file,omitempty" jsonschema:"optional source file reference"`
	AddedBy    string `json:"added_by,omitempty" jsonschema:"who added this memory"`
}

type memoryAddDrawerOutput struct {
	DrawerID   string `json:"drawer_id"`
	MemoryType string `json:"memory_type"`
	Info       string `json:"info"`
}

type memoryDeleteDrawerInput struct {
	DrawerID string `json:"drawer_id" jsonschema:"ID of the drawer to delete"`
}

type memoryDeleteDrawerOutput struct {
	Deleted bool   `json:"deleted"`
	Info    string `json:"info"`
}

type memorySetIdentityInput struct {
	Content string `json:"content" jsonschema:"the identity text for this workspace"`
}

type memorySetIdentityOutput struct {
	Info string `json:"info"`
}

// --- Input/output types: Knowledge Graph tools ---

type memoryKGQueryInput struct {
	Entity    string `json:"entity" jsonschema:"entity name to query"`
	AsOf      string `json:"as_of,omitempty" jsonschema:"optional date filter (YYYY-MM-DD)"`
	Direction string `json:"direction,omitempty" jsonschema:"outgoing, incoming, or both (default both)"`
}

type memoryKGQueryOutput struct {
	Entity  string            `json:"entity"`
	Results []tripleResultOut `json:"results"`
	Count   int               `json:"count"`
}

type tripleResultOut struct {
	Subject   string `json:"subject"`
	Predicate string `json:"predicate"`
	Object    string `json:"object"`
	ValidFrom string `json:"valid_from,omitempty"`
	ValidTo   string `json:"valid_to,omitempty"`
	Direction string `json:"direction"`
	Current   bool   `json:"current"`
}

type memoryKGAddInput struct {
	Subject      string `json:"subject" jsonschema:"subject entity name"`
	Predicate    string `json:"predicate" jsonschema:"relationship type"`
	Object       string `json:"object" jsonschema:"object entity name"`
	ValidFrom    string `json:"valid_from,omitempty" jsonschema:"when the fact became true (YYYY-MM-DD)"`
	SourceCloset string `json:"source_closet,omitempty" jsonschema:"source drawer/closet ID"`
}

type memoryKGAddOutput struct {
	TripleID string `json:"triple_id"`
	Info     string `json:"info"`
}

type memoryKGInvalidateInput struct {
	Subject   string `json:"subject" jsonschema:"subject entity name"`
	Predicate string `json:"predicate" jsonschema:"relationship type"`
	Object    string `json:"object" jsonschema:"object entity name"`
	Ended     string `json:"ended,omitempty" jsonschema:"when the fact ended (YYYY-MM-DD, default now)"`
}

type memoryKGInvalidateOutput struct {
	Info string `json:"info"`
}

type memoryKGTimelineInput struct {
	Entity string `json:"entity,omitempty" jsonschema:"optional entity to filter timeline"`
}

type memoryKGTimelineOutput struct {
	Events []tripleResultOut `json:"events"`
	Count  int               `json:"count"`
}

type memoryKGStatsInput struct{}

type memoryKGStatsOutput struct {
	Entities          int      `json:"entities"`
	Triples           int      `json:"triples"`
	CurrentFacts      int      `json:"current_facts"`
	ExpiredFacts      int      `json:"expired_facts"`
	RelationshipTypes []string `json:"relationship_types"`
}

// --- Input/output types: Diary tools ---

type memoryDiaryWriteInput struct {
	AgentName string `json:"agent_name" jsonschema:"name of the agent writing the diary entry"`
	Entry     string `json:"entry" jsonschema:"the diary entry text"`
	Topic     string `json:"topic,omitempty" jsonschema:"optional topic/tag for the entry"`
}

type memoryDiaryWriteOutput struct {
	DrawerID string `json:"drawer_id"`
	Info     string `json:"info"`
}

type memoryDiaryReadInput struct {
	AgentName string `json:"agent_name" jsonschema:"name of the agent whose diary to read"`
	LastN     int    `json:"last_n,omitempty" jsonschema:"number of recent entries (default 10)"`
}

type memoryDiaryReadOutput struct {
	Entries []diaryEntryBrief `json:"entries"`
	Count   int               `json:"count"`
}

type diaryEntryBrief struct {
	ID      string `json:"id"`
	Content string `json:"content"`
	Hall    string `json:"hall,omitempty"`
	FiledAt string `json:"filed_at"`
}

// --- Handlers ---

func newMemoryStatusHandler(deps *Deps) sdkmcp.ToolHandlerFor[memoryStatusInput, memoryStatusOutput] {
	return func(ctx context.Context, _ *sdkmcp.CallToolRequest, _ memoryStatusInput) (*sdkmcp.CallToolResult, memoryStatusOutput, error) {
		workspaceID := workspaceIDFromContext(ctx)
		if workspaceID == "" {
			return nil, memoryStatusOutput{}, fmt.Errorf("unauthorized: no workspace identity")
		}

		sess := deps.newSession()

		count, err := deps.DrawerRepo.Count(ctx, sess, workspaceID)
		if err != nil {
			return nil, memoryStatusOutput{}, err
		}

		wings, err := deps.DrawerRepo.ListWings(ctx, sess, workspaceID)
		if err != nil {
			return nil, memoryStatusOutput{}, err
		}

		rooms, err := deps.DrawerRepo.ListRooms(ctx, sess, workspaceID, "")
		if err != nil {
			return nil, memoryStatusOutput{}, err
		}

		return nil, memoryStatusOutput{
			TotalDrawers: count,
			Wings:        wings,
			Rooms:        rooms,
		}, nil
	}
}

func newMemoryListWingsHandler(deps *Deps) sdkmcp.ToolHandlerFor[memoryListWingsInput, memoryListWingsOutput] {
	return func(ctx context.Context, _ *sdkmcp.CallToolRequest, _ memoryListWingsInput) (*sdkmcp.CallToolResult, memoryListWingsOutput, error) {
		workspaceID := workspaceIDFromContext(ctx)
		if workspaceID == "" {
			return nil, memoryListWingsOutput{}, fmt.Errorf("unauthorized: no workspace identity")
		}

		sess := deps.newSession()
		wings, err := deps.DrawerRepo.ListWings(ctx, sess, workspaceID)
		if err != nil {
			return nil, memoryListWingsOutput{}, err
		}

		return nil, memoryListWingsOutput{Wings: wings}, nil
	}
}

func newMemoryListRoomsHandler(deps *Deps) sdkmcp.ToolHandlerFor[memoryListRoomsInput, memoryListRoomsOutput] {
	return func(ctx context.Context, _ *sdkmcp.CallToolRequest, input memoryListRoomsInput) (*sdkmcp.CallToolResult, memoryListRoomsOutput, error) {
		workspaceID := workspaceIDFromContext(ctx)
		if workspaceID == "" {
			return nil, memoryListRoomsOutput{}, fmt.Errorf("unauthorized: no workspace identity")
		}

		sess := deps.newSession()
		rooms, err := deps.DrawerRepo.ListRooms(ctx, sess, workspaceID, input.Wing)
		if err != nil {
			return nil, memoryListRoomsOutput{}, err
		}

		return nil, memoryListRoomsOutput{Rooms: rooms}, nil
	}
}

func newMemoryGetTaxonomyHandler(deps *Deps) sdkmcp.ToolHandlerFor[memoryGetTaxonomyInput, memoryGetTaxonomyOutput] {
	return func(ctx context.Context, _ *sdkmcp.CallToolRequest, _ memoryGetTaxonomyInput) (*sdkmcp.CallToolResult, memoryGetTaxonomyOutput, error) {
		workspaceID := workspaceIDFromContext(ctx)
		if workspaceID == "" {
			return nil, memoryGetTaxonomyOutput{}, fmt.Errorf("unauthorized: no workspace identity")
		}

		sess := deps.newSession()
		wings, err := deps.DrawerRepo.ListWings(ctx, sess, workspaceID)
		if err != nil {
			return nil, memoryGetTaxonomyOutput{}, err
		}

		taxonomy := make(map[string]map[string]int, len(wings))
		for _, w := range wings {
			rooms, err := deps.DrawerRepo.ListRooms(ctx, sess, workspaceID, w.Wing)
			if err != nil {
				return nil, memoryGetTaxonomyOutput{}, err
			}
			roomMap := make(map[string]int, len(rooms))
			for _, r := range rooms {
				roomMap[r.Room] = r.Count
			}
			taxonomy[w.Wing] = roomMap
		}

		return nil, memoryGetTaxonomyOutput{Taxonomy: taxonomy}, nil
	}
}

func newMemorySearchHandler(deps *Deps) sdkmcp.ToolHandlerFor[memorySearchInput, memorySearchOutput] {
	return func(ctx context.Context, _ *sdkmcp.CallToolRequest, input memorySearchInput) (*sdkmcp.CallToolResult, memorySearchOutput, error) {
		workspaceID := workspaceIDFromContext(ctx)
		if workspaceID == "" {
			return nil, memorySearchOutput{}, fmt.Errorf("unauthorized: no workspace identity")
		}
		if input.Query == "" {
			return nil, memorySearchOutput{}, fmt.Errorf("query is required")
		}

		limit := input.Limit
		if limit <= 0 {
			limit = 5
		}
		if limit > maxSearchLimit {
			limit = maxSearchLimit
		}

		embedding, err := deps.Embedder.Embed(ctx, input.Query)
		if err != nil {
			return nil, memorySearchOutput{}, fmt.Errorf("embedding failed: %w", err)
		}

		sess := deps.newSession()
		results, err := deps.DrawerRepo.Search(ctx, sess, workspaceID, embedding, input.Wing, input.Room, limit)
		if err != nil {
			return nil, memorySearchOutput{}, err
		}

		briefs := make([]memorySearchResultBrief, 0, len(results))
		for i := range results {
			r := &results[i]
			briefs = append(briefs, memorySearchResultBrief{
				ID:         r.ID,
				Wing:       r.Wing,
				Room:       r.Room,
				Content:    r.Content,
				MemoryType: r.MemoryType,
				Importance: r.Importance,
				Similarity: r.Similarity,
				FiledAt:    r.FiledAt.Format(time.RFC3339),
			})
		}

		return nil, memorySearchOutput{Results: briefs, Count: len(briefs)}, nil
	}
}

func newMemoryCheckDuplicateHandler(deps *Deps) sdkmcp.ToolHandlerFor[memoryCheckDuplicateInput, memoryCheckDuplicateOutput] {
	return func(ctx context.Context, _ *sdkmcp.CallToolRequest, input memoryCheckDuplicateInput) (*sdkmcp.CallToolResult, memoryCheckDuplicateOutput, error) {
		workspaceID := workspaceIDFromContext(ctx)
		if workspaceID == "" {
			return nil, memoryCheckDuplicateOutput{}, fmt.Errorf("unauthorized: no workspace identity")
		}
		if input.Content == "" {
			return nil, memoryCheckDuplicateOutput{}, fmt.Errorf("content is required")
		}

		threshold := input.Threshold
		if threshold <= 0 {
			threshold = 0.9
		}

		embedding, err := deps.Embedder.Embed(ctx, input.Content)
		if err != nil {
			return nil, memoryCheckDuplicateOutput{}, fmt.Errorf("embedding failed: %w", err)
		}

		sess := deps.newSession()
		results, err := deps.DrawerRepo.CheckDuplicate(ctx, sess, workspaceID, embedding, threshold, 5)
		if err != nil {
			return nil, memoryCheckDuplicateOutput{}, err
		}

		briefs := make([]memorySearchResultBrief, 0, len(results))
		for i := range results {
			r := &results[i]
			briefs = append(briefs, memorySearchResultBrief{
				ID:         r.ID,
				Wing:       r.Wing,
				Room:       r.Room,
				Content:    r.Content,
				MemoryType: r.MemoryType,
				Importance: r.Importance,
				Similarity: r.Similarity,
				FiledAt:    r.FiledAt.Format(time.RFC3339),
			})
		}

		return nil, memoryCheckDuplicateOutput{
			Duplicates: briefs,
			HasDupe:    len(briefs) > 0,
		}, nil
	}
}

func newMemoryTraverseHandler(deps *Deps) sdkmcp.ToolHandlerFor[memoryTraverseInput, memoryTraverseOutput] {
	return func(ctx context.Context, _ *sdkmcp.CallToolRequest, input memoryTraverseInput) (*sdkmcp.CallToolResult, memoryTraverseOutput, error) {
		workspaceID := workspaceIDFromContext(ctx)
		if workspaceID == "" {
			return nil, memoryTraverseOutput{}, fmt.Errorf("unauthorized: no workspace identity")
		}
		if input.StartRoom == "" {
			return nil, memoryTraverseOutput{}, fmt.Errorf("start_room is required")
		}

		maxHops := input.MaxHops
		if maxHops <= 0 {
			maxHops = 3
		}
		if maxHops > 5 {
			maxHops = 5
		}

		sess := deps.newSession()
		results, err := deps.PalaceGraph.Traverse(ctx, sess, workspaceID, input.StartRoom, maxHops)
		if err != nil {
			return nil, memoryTraverseOutput{}, err
		}

		briefs := make([]traversalBrief, 0, len(results))
		for _, r := range results {
			briefs = append(briefs, traversalBrief{
				Room:         r.Room,
				Wings:        r.Wings,
				Halls:        r.Halls,
				Count:        r.Count,
				Hop:          r.Hop,
				ConnectedVia: r.ConnectedVia,
			})
		}

		return nil, memoryTraverseOutput{Rooms: briefs, Count: len(briefs)}, nil
	}
}

func newMemoryFindTunnelsHandler(deps *Deps) sdkmcp.ToolHandlerFor[memoryFindTunnelsInput, memoryFindTunnelsOutput] {
	return func(ctx context.Context, _ *sdkmcp.CallToolRequest, input memoryFindTunnelsInput) (*sdkmcp.CallToolResult, memoryFindTunnelsOutput, error) {
		workspaceID := workspaceIDFromContext(ctx)
		if workspaceID == "" {
			return nil, memoryFindTunnelsOutput{}, fmt.Errorf("unauthorized: no workspace identity")
		}

		sess := deps.newSession()
		tunnels, err := deps.PalaceGraph.FindTunnels(ctx, sess, workspaceID, input.WingA, input.WingB)
		if err != nil {
			return nil, memoryFindTunnelsOutput{}, err
		}

		briefs := make([]tunnelBrief, 0, len(tunnels))
		for _, t := range tunnels {
			briefs = append(briefs, tunnelBrief{
				Room:  t.Room,
				Wings: t.Wings,
				Halls: t.Halls,
				Count: t.Count,
			})
		}

		return nil, memoryFindTunnelsOutput{Tunnels: briefs, Count: len(briefs)}, nil
	}
}

func newMemoryGraphStatsHandler(deps *Deps) sdkmcp.ToolHandlerFor[memoryGraphStatsInput, memoryGraphStatsOutput] {
	return func(ctx context.Context, _ *sdkmcp.CallToolRequest, _ memoryGraphStatsInput) (*sdkmcp.CallToolResult, memoryGraphStatsOutput, error) {
		workspaceID := workspaceIDFromContext(ctx)
		if workspaceID == "" {
			return nil, memoryGraphStatsOutput{}, fmt.Errorf("unauthorized: no workspace identity")
		}

		sess := deps.newSession()
		stats, err := deps.PalaceGraph.GraphStats(ctx, sess, workspaceID)
		if err != nil {
			return nil, memoryGraphStatsOutput{}, err
		}

		topTunnels := make([]tunnelBrief, 0, len(stats.TopTunnels))
		for _, t := range stats.TopTunnels {
			topTunnels = append(topTunnels, tunnelBrief{
				Room:  t.Room,
				Wings: t.Wings,
				Halls: t.Halls,
				Count: t.Count,
			})
		}

		return nil, memoryGraphStatsOutput{
			TotalRooms:   stats.TotalRooms,
			TunnelRooms:  stats.TunnelRooms,
			TotalEdges:   stats.TotalEdges,
			RoomsPerWing: stats.RoomsPerWing,
			TopTunnels:   topTunnels,
		}, nil
	}
}

// --- Write tool handlers ---

func newMemoryAddDrawerHandler(deps *Deps) sdkmcp.ToolHandlerFor[memoryAddDrawerInput, memoryAddDrawerOutput] {
	return func(ctx context.Context, _ *sdkmcp.CallToolRequest, input memoryAddDrawerInput) (*sdkmcp.CallToolResult, memoryAddDrawerOutput, error) {
		workspaceID := workspaceIDFromContext(ctx)
		if workspaceID == "" {
			return nil, memoryAddDrawerOutput{}, fmt.Errorf("unauthorized: no workspace identity")
		}
		if input.Wing == "" || input.Room == "" || input.Content == "" {
			return nil, memoryAddDrawerOutput{Info: "wing, room, and content are required"}, nil
		}
		if len(input.Content) > memory.MaxContentLength {
			return nil, memoryAddDrawerOutput{Info: fmt.Sprintf("content exceeds max length of %d", memory.MaxContentLength)}, nil
		}

		// Generate drawer ID.
		hash := sha256.Sum256([]byte(input.Content))
		drawerID := fmt.Sprintf("drawer_%s_%s_%x", input.Wing, input.Room, hash[:8])

		// Classify memory type and derive importance.
		memoryType := "general"
		importance := defaultImportance
		if deps.Classifier != nil {
			classified := deps.Classifier.Classify(input.Content, classifierMinConfidence)
			if len(classified) > 0 {
				memoryType = classified[0].MemoryType
				for _, c := range classified {
					if c.Confidence*importanceScale > importance {
						importance = c.Confidence * importanceScale
					}
				}
			}
		}

		// Generate embedding (best-effort).
		var embedding []float32
		if deps.Embedder != nil {
			emb, err := deps.Embedder.Embed(ctx, input.Content)
			if err != nil {
				deps.Logger.WarnContext(ctx, "embedding failed for drawer, storing without embedding", "error", err)
			} else {
				embedding = emb
			}
		}

		d := &memory.Drawer{
			ID:          drawerID,
			WorkspaceID: workspaceID,
			Wing:        input.Wing,
			Room:        input.Room,
			Content:     input.Content,
			MemoryType:  memoryType,
			Importance:  importance,
			SourceFile:  input.SourceFile,
			AddedBy:     input.AddedBy,
			FiledAt:     time.Now().UTC(),
			CreatedAt:   time.Now().UTC(),
		}

		sess := deps.newSession()
		if err := deps.DrawerRepo.Add(ctx, sess, d, embedding); err != nil {
			deps.Logger.ErrorContext(ctx, "failed to store drawer", "error", err)
			return nil, memoryAddDrawerOutput{Info: "failed to store memory"}, nil
		}

		// Reinforce similar memories.
		if len(embedding) > 0 {
			similar, _ := deps.DrawerRepo.Search(ctx, sess, workspaceID, embedding, "", "", 5)
			for i := range similar {
				if similar[i].ID != drawerID && similar[i].Similarity > memory.ReinforcementThreshold {
					_ = deps.DrawerRepo.BoostImportance(ctx, sess, workspaceID, similar[i].ID, memory.ReinforcementBoost, memory.MaxImportance)
				}
			}
		}

		return nil, memoryAddDrawerOutput{
			DrawerID:   drawerID,
			MemoryType: memoryType,
			Info:       "drawer added",
		}, nil
	}
}

func newMemoryDeleteDrawerHandler(deps *Deps) sdkmcp.ToolHandlerFor[memoryDeleteDrawerInput, memoryDeleteDrawerOutput] {
	return func(ctx context.Context, _ *sdkmcp.CallToolRequest, input memoryDeleteDrawerInput) (*sdkmcp.CallToolResult, memoryDeleteDrawerOutput, error) {
		workspaceID := workspaceIDFromContext(ctx)
		if workspaceID == "" {
			return nil, memoryDeleteDrawerOutput{}, fmt.Errorf("unauthorized: no workspace identity")
		}
		if input.DrawerID == "" {
			return nil, memoryDeleteDrawerOutput{Info: "drawer_id is required"}, nil
		}

		sess := deps.newSession()
		if err := deps.DrawerRepo.Delete(ctx, sess, workspaceID, input.DrawerID); err != nil {
			deps.Logger.ErrorContext(ctx, "failed to delete drawer", "error", err)
			return nil, memoryDeleteDrawerOutput{Info: "failed to delete drawer"}, nil
		}

		return nil, memoryDeleteDrawerOutput{Deleted: true, Info: "drawer deleted"}, nil
	}
}

func newMemorySetIdentityHandler(deps *Deps) sdkmcp.ToolHandlerFor[memorySetIdentityInput, memorySetIdentityOutput] {
	return func(ctx context.Context, _ *sdkmcp.CallToolRequest, input memorySetIdentityInput) (*sdkmcp.CallToolResult, memorySetIdentityOutput, error) {
		workspaceID := workspaceIDFromContext(ctx)
		if workspaceID == "" {
			return nil, memorySetIdentityOutput{}, fmt.Errorf("unauthorized: no workspace identity")
		}
		if input.Content == "" {
			return nil, memorySetIdentityOutput{Info: "content is required"}, nil
		}
		if len(input.Content) > memory.MaxIdentityLength {
			return nil, memorySetIdentityOutput{Info: fmt.Sprintf("content exceeds max length of %d", memory.MaxIdentityLength)}, nil
		}

		sess := deps.newSession()
		_, err := sess.InsertBySql(
			`INSERT INTO memory_identities (workspace_id, content, updated_at)
			 VALUES (?, ?, NOW())
			 ON CONFLICT (workspace_id) DO UPDATE SET content = EXCLUDED.content, updated_at = NOW()`,
			workspaceID, input.Content,
		).ExecContext(ctx)
		if err != nil {
			deps.Logger.ErrorContext(ctx, "failed to set identity", "error", err)
			return nil, memorySetIdentityOutput{Info: "failed to update identity"}, nil
		}

		return nil, memorySetIdentityOutput{Info: "identity set"}, nil
	}
}

// --- Knowledge Graph handlers ---

func newMemoryKGQueryHandler(deps *Deps) sdkmcp.ToolHandlerFor[memoryKGQueryInput, memoryKGQueryOutput] {
	return func(ctx context.Context, _ *sdkmcp.CallToolRequest, input memoryKGQueryInput) (*sdkmcp.CallToolResult, memoryKGQueryOutput, error) {
		workspaceID := workspaceIDFromContext(ctx)
		if workspaceID == "" {
			return nil, memoryKGQueryOutput{}, fmt.Errorf("unauthorized: no workspace identity")
		}
		if input.Entity == "" {
			return nil, memoryKGQueryOutput{}, fmt.Errorf("entity is required")
		}

		direction := input.Direction
		if direction == "" {
			direction = "both"
		}

		sess := deps.newSession()
		results, err := deps.KG.QueryEntity(ctx, sess, workspaceID, input.Entity, input.AsOf, direction)
		if err != nil {
			return nil, memoryKGQueryOutput{}, err
		}

		out := make([]tripleResultOut, 0, len(results))
		for i := range results {
			r := &results[i]
			tr := tripleResultOut{
				Subject:   r.SubjectName,
				Predicate: r.Predicate,
				Object:    r.ObjectName,
				Direction: r.Direction,
				Current:   r.Current,
			}
			if r.ValidFrom != nil {
				tr.ValidFrom = *r.ValidFrom
			}
			if r.ValidTo != nil {
				tr.ValidTo = *r.ValidTo
			}
			out = append(out, tr)
		}

		return nil, memoryKGQueryOutput{Entity: input.Entity, Results: out, Count: len(out)}, nil
	}
}

func newMemoryKGAddHandler(deps *Deps) sdkmcp.ToolHandlerFor[memoryKGAddInput, memoryKGAddOutput] {
	return func(ctx context.Context, _ *sdkmcp.CallToolRequest, input memoryKGAddInput) (*sdkmcp.CallToolResult, memoryKGAddOutput, error) {
		workspaceID := workspaceIDFromContext(ctx)
		if workspaceID == "" {
			return nil, memoryKGAddOutput{}, fmt.Errorf("unauthorized: no workspace identity")
		}
		if input.Subject == "" || input.Predicate == "" || input.Object == "" {
			return nil, memoryKGAddOutput{Info: "subject, predicate, and object are required"}, nil
		}

		t := &memory.Triple{
			WorkspaceID:  workspaceID,
			Subject:      input.Subject,
			Predicate:    input.Predicate,
			Object:       input.Object,
			SourceCloset: input.SourceCloset,
		}
		if input.ValidFrom != "" {
			t.ValidFrom = &input.ValidFrom
		}

		sess := deps.newSession()
		tripleID, err := deps.KG.AddTriple(ctx, sess, workspaceID, t)
		if err != nil {
			deps.Logger.ErrorContext(ctx, "failed to add triple", "error", err)
			return nil, memoryKGAddOutput{Info: "failed to update knowledge graph"}, nil
		}

		return nil, memoryKGAddOutput{TripleID: tripleID, Info: "triple added"}, nil
	}
}

func newMemoryKGInvalidateHandler(deps *Deps) sdkmcp.ToolHandlerFor[memoryKGInvalidateInput, memoryKGInvalidateOutput] {
	return func(ctx context.Context, _ *sdkmcp.CallToolRequest, input memoryKGInvalidateInput) (*sdkmcp.CallToolResult, memoryKGInvalidateOutput, error) {
		workspaceID := workspaceIDFromContext(ctx)
		if workspaceID == "" {
			return nil, memoryKGInvalidateOutput{}, fmt.Errorf("unauthorized: no workspace identity")
		}
		if input.Subject == "" || input.Predicate == "" || input.Object == "" {
			return nil, memoryKGInvalidateOutput{Info: "subject, predicate, and object are required"}, nil
		}

		ended := input.Ended
		if ended == "" {
			ended = time.Now().Format("2006-01-02")
		}

		sess := deps.newSession()
		if err := deps.KG.Invalidate(ctx, sess, workspaceID, input.Subject, input.Predicate, input.Object, ended); err != nil {
			deps.Logger.ErrorContext(ctx, "failed to invalidate triple", "error", err)
			return nil, memoryKGInvalidateOutput{Info: "failed to update knowledge graph"}, nil
		}

		return nil, memoryKGInvalidateOutput{Info: "triple invalidated"}, nil
	}
}

func newMemoryKGTimelineHandler(deps *Deps) sdkmcp.ToolHandlerFor[memoryKGTimelineInput, memoryKGTimelineOutput] {
	return func(ctx context.Context, _ *sdkmcp.CallToolRequest, input memoryKGTimelineInput) (*sdkmcp.CallToolResult, memoryKGTimelineOutput, error) {
		workspaceID := workspaceIDFromContext(ctx)
		if workspaceID == "" {
			return nil, memoryKGTimelineOutput{}, fmt.Errorf("unauthorized: no workspace identity")
		}

		sess := deps.newSession()
		results, err := deps.KG.Timeline(ctx, sess, workspaceID, input.Entity)
		if err != nil {
			return nil, memoryKGTimelineOutput{}, err
		}

		out := make([]tripleResultOut, 0, len(results))
		for i := range results {
			r := &results[i]
			tr := tripleResultOut{
				Subject:   r.SubjectName,
				Predicate: r.Predicate,
				Object:    r.ObjectName,
				Direction: r.Direction,
				Current:   r.Current,
			}
			if r.ValidFrom != nil {
				tr.ValidFrom = *r.ValidFrom
			}
			if r.ValidTo != nil {
				tr.ValidTo = *r.ValidTo
			}
			out = append(out, tr)
		}

		return nil, memoryKGTimelineOutput{Events: out, Count: len(out)}, nil
	}
}

func newMemoryKGStatsHandler(deps *Deps) sdkmcp.ToolHandlerFor[memoryKGStatsInput, memoryKGStatsOutput] {
	return func(ctx context.Context, _ *sdkmcp.CallToolRequest, _ memoryKGStatsInput) (*sdkmcp.CallToolResult, memoryKGStatsOutput, error) {
		workspaceID := workspaceIDFromContext(ctx)
		if workspaceID == "" {
			return nil, memoryKGStatsOutput{}, fmt.Errorf("unauthorized: no workspace identity")
		}

		sess := deps.newSession()
		stats, err := deps.KG.Stats(ctx, sess, workspaceID)
		if err != nil {
			return nil, memoryKGStatsOutput{}, err
		}

		return nil, memoryKGStatsOutput{
			Entities:          stats.Entities,
			Triples:           stats.Triples,
			CurrentFacts:      stats.CurrentFacts,
			ExpiredFacts:      stats.ExpiredFacts,
			RelationshipTypes: stats.RelationshipTypes,
		}, nil
	}
}

// --- Diary handlers ---

func newMemoryDiaryWriteHandler(deps *Deps) sdkmcp.ToolHandlerFor[memoryDiaryWriteInput, memoryDiaryWriteOutput] {
	return func(ctx context.Context, _ *sdkmcp.CallToolRequest, input memoryDiaryWriteInput) (*sdkmcp.CallToolResult, memoryDiaryWriteOutput, error) {
		workspaceID := workspaceIDFromContext(ctx)
		if workspaceID == "" {
			return nil, memoryDiaryWriteOutput{}, fmt.Errorf("unauthorized: no workspace identity")
		}
		if input.AgentName == "" || input.Entry == "" {
			return nil, memoryDiaryWriteOutput{Info: "agent_name and entry are required"}, nil
		}
		if len(input.Entry) > memory.MaxContentLength {
			return nil, memoryDiaryWriteOutput{Info: fmt.Sprintf("entry exceeds max length of %d", memory.MaxContentLength)}, nil
		}

		agentName := strings.ToLower(strings.TrimSpace(input.AgentName))
		agentName = strings.ReplaceAll(agentName, " ", "_")
		agentName = strings.ReplaceAll(agentName, "'", "")
		wing := fmt.Sprintf("wing_%s", agentName)
		room := "diary"

		hash := sha256.Sum256([]byte(input.Entry))
		drawerID := fmt.Sprintf("drawer_%s_%s_%x", wing, room, hash[:8])

		// Best-effort embedding.
		var embedding []float32
		if deps.Embedder != nil {
			emb, err := deps.Embedder.Embed(ctx, input.Entry)
			if err != nil {
				deps.Logger.WarnContext(ctx, "embedding failed for diary entry, storing without embedding", "error", err)
			} else {
				embedding = emb
			}
		}

		d := &memory.Drawer{
			ID:          drawerID,
			WorkspaceID: workspaceID,
			Wing:        wing,
			Room:        room,
			Hall:        input.Topic,
			Content:     input.Entry,
			MemoryType:  "diary",
			Importance:  defaultImportance,
			AddedBy:     input.AgentName,
			FiledAt:     time.Now().UTC(),
			CreatedAt:   time.Now().UTC(),
		}

		sess := deps.newSession()
		if err := deps.DrawerRepo.Add(ctx, sess, d, embedding); err != nil {
			deps.Logger.ErrorContext(ctx, "failed to write diary entry", "error", err)
			return nil, memoryDiaryWriteOutput{Info: "failed to write diary entry"}, nil
		}

		return nil, memoryDiaryWriteOutput{DrawerID: drawerID, Info: "diary entry written"}, nil
	}
}

func newMemoryDiaryReadHandler(deps *Deps) sdkmcp.ToolHandlerFor[memoryDiaryReadInput, memoryDiaryReadOutput] {
	return func(ctx context.Context, _ *sdkmcp.CallToolRequest, input memoryDiaryReadInput) (*sdkmcp.CallToolResult, memoryDiaryReadOutput, error) {
		workspaceID := workspaceIDFromContext(ctx)
		if workspaceID == "" {
			return nil, memoryDiaryReadOutput{}, fmt.Errorf("unauthorized: no workspace identity")
		}
		if input.AgentName == "" {
			return nil, memoryDiaryReadOutput{}, fmt.Errorf("agent_name is required")
		}

		lastN := input.LastN
		if lastN <= 0 {
			lastN = 10
		}
		if lastN > 100 {
			lastN = 100
		}

		agentName := strings.ToLower(strings.TrimSpace(input.AgentName))
		agentName = strings.ReplaceAll(agentName, " ", "_")
		agentName = strings.ReplaceAll(agentName, "'", "")
		wing := fmt.Sprintf("wing_%s", agentName)

		sess := deps.newSession()
		drawers, err := deps.DrawerRepo.GetByWingRoom(ctx, sess, workspaceID, wing, "diary", lastN)
		if err != nil {
			return nil, memoryDiaryReadOutput{}, err
		}

		entries := make([]diaryEntryBrief, 0, len(drawers))
		for i := range drawers {
			d := &drawers[i]
			entries = append(entries, diaryEntryBrief{
				ID:      d.ID,
				Content: d.Content,
				Hall:    d.Hall,
				FiledAt: d.FiledAt.Format(time.RFC3339),
			})
		}

		return nil, memoryDiaryReadOutput{Entries: entries, Count: len(entries)}, nil
	}
}

// registerMemoryTools adds all memory palace tools to the MCP server.
func registerMemoryTools(server *sdkmcp.Server, deps *Deps) {
	// Read tools
	sdkmcp.AddTool(server, &sdkmcp.Tool{
		Name:        "memory_status",
		Description: "Get memory palace status: total drawer count, wings, and rooms.",
	}, newMemoryStatusHandler(deps))

	sdkmcp.AddTool(server, &sdkmcp.Tool{
		Name:        "memory_list_wings",
		Description: "List all wings (top-level categories) in the memory palace with drawer counts.",
	}, newMemoryListWingsHandler(deps))

	sdkmcp.AddTool(server, &sdkmcp.Tool{
		Name:        "memory_list_rooms",
		Description: "List rooms in the memory palace, optionally filtered by wing.",
	}, newMemoryListRoomsHandler(deps))

	sdkmcp.AddTool(server, &sdkmcp.Tool{
		Name:        "memory_get_taxonomy",
		Description: "Get the full taxonomy tree: wings with their rooms and drawer counts.",
	}, newMemoryGetTaxonomyHandler(deps))

	sdkmcp.AddTool(server, &sdkmcp.Tool{
		Name:        "memory_search",
		Description: "Semantic search through memory drawers by meaning. Returns the most relevant memories for a query.",
	}, newMemorySearchHandler(deps))

	sdkmcp.AddTool(server, &sdkmcp.Tool{
		Name:        "memory_check_duplicate",
		Description: "Check if content already exists in memory by semantic similarity. Returns similar existing drawers above the threshold.",
	}, newMemoryCheckDuplicateHandler(deps))

	sdkmcp.AddTool(server, &sdkmcp.Tool{
		Name:        "memory_traverse",
		Description: "Walk the memory palace graph from a starting room via BFS, discovering connected rooms across wings.",
	}, newMemoryTraverseHandler(deps))

	sdkmcp.AddTool(server, &sdkmcp.Tool{
		Name:        "memory_find_tunnels",
		Description: "Find rooms that bridge two or more wings (cross-cutting concepts). Optionally filter by specific wing pair.",
	}, newMemoryFindTunnelsHandler(deps))

	sdkmcp.AddTool(server, &sdkmcp.Tool{
		Name:        "memory_graph_stats",
		Description: "Get palace graph statistics: total rooms, tunnel rooms, edges, and rooms per wing.",
	}, newMemoryGraphStatsHandler(deps))

	// Write tools
	sdkmcp.AddTool(server, &sdkmcp.Tool{
		Name:        "memory_add_drawer",
		Description: "Store a new memory in the palace. Specify wing and room for organization. Content is auto-classified and embedded for semantic search.",
	}, newMemoryAddDrawerHandler(deps))

	sdkmcp.AddTool(server, &sdkmcp.Tool{
		Name:        "memory_delete_drawer",
		Description: "Delete a memory drawer by its ID.",
	}, newMemoryDeleteDrawerHandler(deps))

	sdkmcp.AddTool(server, &sdkmcp.Tool{
		Name:        "memory_set_identity",
		Description: "Set or update the workspace identity text. This is the L0 layer: a concise description of who the user is and their core context.",
	}, newMemorySetIdentityHandler(deps))

	// Knowledge graph tools
	sdkmcp.AddTool(server, &sdkmcp.Tool{
		Name:        "memory_kg_query",
		Description: "Query the knowledge graph for all relationships of an entity. Returns incoming, outgoing, or both directions.",
	}, newMemoryKGQueryHandler(deps))

	sdkmcp.AddTool(server, &sdkmcp.Tool{
		Name:        "memory_kg_add",
		Description: "Add a relationship triple to the knowledge graph. Subject and object entities are auto-created if they don't exist.",
	}, newMemoryKGAddHandler(deps))

	sdkmcp.AddTool(server, &sdkmcp.Tool{
		Name:        "memory_kg_invalidate",
		Description: "Mark a knowledge graph relationship as no longer valid. Sets the end date on the triple.",
	}, newMemoryKGInvalidateHandler(deps))

	sdkmcp.AddTool(server, &sdkmcp.Tool{
		Name:        "memory_kg_timeline",
		Description: "View knowledge graph facts in chronological order. Optionally filter by entity.",
	}, newMemoryKGTimelineHandler(deps))

	sdkmcp.AddTool(server, &sdkmcp.Tool{
		Name:        "memory_kg_stats",
		Description: "Get knowledge graph statistics: entity count, triple count, current vs expired facts, and relationship types.",
	}, newMemoryKGStatsHandler(deps))

	// Diary tools
	sdkmcp.AddTool(server, &sdkmcp.Tool{
		Name:        "memory_diary_write",
		Description: "Write a diary entry for an agent. Diary entries are stored as drawers in wing_{agent_name}/diary.",
	}, newMemoryDiaryWriteHandler(deps))

	sdkmcp.AddTool(server, &sdkmcp.Tool{
		Name:        "memory_diary_read",
		Description: "Read recent diary entries for an agent. Returns entries newest-first.",
	}, newMemoryDiaryReadHandler(deps))
}
