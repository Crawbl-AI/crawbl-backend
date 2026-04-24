package mcp

import (
	"context"
	"crypto/sha256"
	"fmt"
	"strings"
	"time"

	"github.com/gocraft/dbr/v2"
	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"

	mcpv1 "github.com/Crawbl-AI/crawbl-backend/internal/generated/proto/mcp/v1"
	"github.com/Crawbl-AI/crawbl-backend/internal/orchestrator/memory"
	"github.com/Crawbl-AI/crawbl-backend/internal/orchestrator/memory/repo/drawerrepo"
)

// --- Handlers ---

func newMemoryStatusHandler(deps *Deps) sdkmcp.ToolHandlerFor[memoryStatusInput, *mcpv1.MemoryStatusOutput] {
	return authedTool(deps, func(ctx context.Context, sess *dbr.Session, workspaceID string, _ memoryStatusInput) (*sdkmcp.CallToolResult, *mcpv1.MemoryStatusOutput, error) {
		count, err := deps.DrawerRepo.Count(ctx, sess, workspaceID)
		if err != nil {
			return nil, nil, err
		}

		wings, err := deps.DrawerRepo.ListWings(ctx, sess, workspaceID)
		if err != nil {
			return nil, nil, err
		}

		rooms, err := deps.DrawerRepo.ListRooms(ctx, sess, workspaceID, "")
		if err != nil {
			return nil, nil, err
		}

		return nil, &mcpv1.MemoryStatusOutput{
			TotalDrawers: int32(count),
			Wings:        toProtoWingCounts(wings),
			Rooms:        toProtoRoomCounts(rooms),
		}, nil
	})
}

func newMemoryListWingsHandler(deps *Deps) sdkmcp.ToolHandlerFor[memoryListWingsInput, *mcpv1.MemoryListWingsOutput] {
	return authedTool(deps, func(ctx context.Context, sess *dbr.Session, workspaceID string, _ memoryListWingsInput) (*sdkmcp.CallToolResult, *mcpv1.MemoryListWingsOutput, error) {
		wings, err := deps.DrawerRepo.ListWings(ctx, sess, workspaceID)
		if err != nil {
			return nil, nil, err
		}

		return nil, &mcpv1.MemoryListWingsOutput{Wings: toProtoWingCounts(wings)}, nil
	})
}

func newMemoryListRoomsHandler(deps *Deps) sdkmcp.ToolHandlerFor[memoryListRoomsInput, *mcpv1.MemoryListRoomsOutput] {
	return authedTool(deps, func(ctx context.Context, sess *dbr.Session, workspaceID string, input memoryListRoomsInput) (*sdkmcp.CallToolResult, *mcpv1.MemoryListRoomsOutput, error) {
		rooms, err := deps.DrawerRepo.ListRooms(ctx, sess, workspaceID, input.Wing)
		if err != nil {
			return nil, nil, err
		}

		return nil, &mcpv1.MemoryListRoomsOutput{Rooms: toProtoRoomCounts(rooms)}, nil
	})
}

func newMemoryGetTaxonomyHandler(deps *Deps) sdkmcp.ToolHandlerFor[memoryGetTaxonomyInput, *mcpv1.MemoryGetTaxonomyOutput] {
	return authedTool(deps, func(ctx context.Context, sess *dbr.Session, workspaceID string, _ memoryGetTaxonomyInput) (*sdkmcp.CallToolResult, *mcpv1.MemoryGetTaxonomyOutput, error) {
		wings, err := deps.DrawerRepo.ListWings(ctx, sess, workspaceID)
		if err != nil {
			return nil, nil, err
		}

		taxonomy := make(map[string]*mcpv1.MemoryTaxonomyWing, len(wings))
		for _, w := range wings {
			rooms, err := deps.DrawerRepo.ListRooms(ctx, sess, workspaceID, w.Wing)
			if err != nil {
				return nil, nil, err
			}
			roomMap := make(map[string]int32, len(rooms))
			for _, r := range rooms {
				roomMap[r.Room] = int32(r.Count)
			}
			taxonomy[w.Wing] = &mcpv1.MemoryTaxonomyWing{Rooms: roomMap}
		}

		return nil, &mcpv1.MemoryGetTaxonomyOutput{Taxonomy: taxonomy}, nil
	})
}

func newMemorySearchHandler(deps *Deps) sdkmcp.ToolHandlerFor[memorySearchInput, *mcpv1.MemorySearchOutput] {
	return authedTool(deps, func(ctx context.Context, sess *dbr.Session, workspaceID string, input memorySearchInput) (*sdkmcp.CallToolResult, *mcpv1.MemorySearchOutput, error) {
		if input.Query == "" {
			return nil, nil, fmt.Errorf("query is required")
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
			return nil, nil, fmt.Errorf("embedding failed: %w", err)
		}

		results, err := deps.DrawerRepo.Search(ctx, sess, drawerrepo.SearchOpts{
				WorkspaceID:    workspaceID,
				QueryEmbedding: embedding,
				Wing:           input.Wing,
				Room:           input.Room,
				Limit:          limit,
			})
		if err != nil {
			return nil, nil, err
		}

		briefs := toMemorySearchResultBriefs(results)
		return nil, &mcpv1.MemorySearchOutput{Results: briefs, Count: int32(len(briefs))}, nil
	})
}

func newMemoryCheckDuplicateHandler(deps *Deps) sdkmcp.ToolHandlerFor[memoryCheckDuplicateInput, *mcpv1.MemoryCheckDuplicateOutput] {
	return authedTool(deps, func(ctx context.Context, sess *dbr.Session, workspaceID string, input memoryCheckDuplicateInput) (*sdkmcp.CallToolResult, *mcpv1.MemoryCheckDuplicateOutput, error) {
		if input.Content == "" {
			return nil, nil, fmt.Errorf("content is required")
		}

		threshold := input.Threshold
		if threshold <= 0 {
			threshold = 0.9
		}

		embedding, err := deps.Embedder.Embed(ctx, input.Content)
		if err != nil {
			return nil, nil, fmt.Errorf("embedding failed: %w", err)
		}

		results, err := deps.DrawerRepo.CheckDuplicate(ctx, sess, workspaceID, embedding, threshold, 5)
		if err != nil {
			return nil, nil, err
		}

		briefs := toMemorySearchResultBriefs(results)
		return nil, &mcpv1.MemoryCheckDuplicateOutput{
			Duplicates:   briefs,
			HasDuplicate: len(briefs) > 0,
		}, nil
	})
}

func newMemoryTraverseHandler(deps *Deps) sdkmcp.ToolHandlerFor[memoryTraverseInput, *mcpv1.MemoryTraverseOutput] {
	return authedTool(deps, func(ctx context.Context, sess *dbr.Session, workspaceID string, input memoryTraverseInput) (*sdkmcp.CallToolResult, *mcpv1.MemoryTraverseOutput, error) {
		if input.StartRoom == "" {
			return nil, nil, fmt.Errorf("start_room is required")
		}

		maxHops := input.MaxHops
		if maxHops <= 0 {
			maxHops = 3
		}
		if maxHops > 5 {
			maxHops = 5
		}

		results, err := deps.PalaceGraph.Traverse(ctx, sess, workspaceID, input.StartRoom, maxHops)
		if err != nil {
			return nil, nil, err
		}

		briefs := make([]*mcpv1.TraversalBrief, 0, len(results))
		for _, r := range results {
			briefs = append(briefs, &mcpv1.TraversalBrief{
				Room:         r.Room,
				Wings:        r.Wings,
				Halls:        r.Halls,
				Count:        int32(r.Count),
				Hop:          int32(r.Hop),
				ConnectedVia: r.ConnectedVia,
			})
		}

		return nil, &mcpv1.MemoryTraverseOutput{Rooms: briefs, Count: int32(len(briefs))}, nil
	})
}

func newMemoryFindTunnelsHandler(deps *Deps) sdkmcp.ToolHandlerFor[memoryFindTunnelsInput, *mcpv1.MemoryFindTunnelsOutput] {
	return authedTool(deps, func(ctx context.Context, sess *dbr.Session, workspaceID string, input memoryFindTunnelsInput) (*sdkmcp.CallToolResult, *mcpv1.MemoryFindTunnelsOutput, error) {
		tunnels, err := deps.PalaceGraph.FindTunnels(ctx, sess, workspaceID, input.WingA, input.WingB)
		if err != nil {
			return nil, nil, err
		}

		briefs := make([]*mcpv1.TunnelBrief, 0, len(tunnels))
		for _, t := range tunnels {
			briefs = append(briefs, &mcpv1.TunnelBrief{
				Room:  t.Room,
				Wings: t.Wings,
				Halls: t.Halls,
				Count: int32(t.Count),
			})
		}

		return nil, &mcpv1.MemoryFindTunnelsOutput{Tunnels: briefs, Count: int32(len(briefs))}, nil
	})
}

func newMemoryGraphStatsHandler(deps *Deps) sdkmcp.ToolHandlerFor[memoryGraphStatsInput, *mcpv1.MemoryGraphStatsOutput] {
	return authedTool(deps, func(ctx context.Context, sess *dbr.Session, workspaceID string, _ memoryGraphStatsInput) (*sdkmcp.CallToolResult, *mcpv1.MemoryGraphStatsOutput, error) {
		stats, err := deps.PalaceGraph.GraphStats(ctx, sess, workspaceID)
		if err != nil {
			return nil, nil, err
		}

		topTunnels := make([]*mcpv1.TunnelBrief, 0, len(stats.TopTunnels))
		for _, t := range stats.TopTunnels {
			topTunnels = append(topTunnels, &mcpv1.TunnelBrief{
				Room:  t.Room,
				Wings: t.Wings,
				Halls: t.Halls,
				Count: int32(t.Count),
			})
		}

		roomsPerWing := make(map[string]int32, len(stats.RoomsPerWing))
		for k, v := range stats.RoomsPerWing {
			roomsPerWing[k] = int32(v)
		}

		return nil, &mcpv1.MemoryGraphStatsOutput{
			TotalRooms:   int32(stats.TotalRooms),
			TunnelRooms:  int32(stats.TunnelRooms),
			TotalEdges:   int32(stats.TotalEdges),
			RoomsPerWing: roomsPerWing,
			TopTunnels:   topTunnels,
		}, nil
	})
}

// bestEffortEmbed generates an embedding vector for content, returning nil on
// failure or when no embedder is configured.
func bestEffortEmbed(ctx context.Context, deps *Deps, content string) []float32 {
	if deps.Embedder == nil {
		return nil
	}
	emb, err := deps.Embedder.Embed(ctx, content)
	if err != nil {
		deps.Logger.WarnContext(ctx, "embedding failed for drawer, storing without embedding", "error", err)
		return nil
	}
	return emb
}

// --- Write tool handlers ---

func newMemoryAddDrawerHandler(deps *Deps) sdkmcp.ToolHandlerFor[memoryAddDrawerInput, *mcpv1.MemoryAddDrawerOutput] {
	return authedTool(deps, func(ctx context.Context, sess *dbr.Session, workspaceID string, input memoryAddDrawerInput) (*sdkmcp.CallToolResult, *mcpv1.MemoryAddDrawerOutput, error) {
		if input.Wing == "" || input.Room == "" || input.Content == "" {
			return nil, &mcpv1.MemoryAddDrawerOutput{Info: "wing, room, and content are required"}, nil
		}
		if len(input.Content) > memory.MaxContentLength {
			return nil, &mcpv1.MemoryAddDrawerOutput{Info: fmt.Sprintf("content exceeds max length of %d", memory.MaxContentLength)}, nil
		}

		// Reject trivial/noise content (greetings, short filler).
		if deps.NoisePattern != nil && (len(input.Content) < deps.NoiseMinLength || deps.NoisePattern.MatchString(strings.TrimSpace(input.Content))) {
			return nil, &mcpv1.MemoryAddDrawerOutput{Info: "content too short or trivial to store"}, nil
		}

		// Generate drawer ID.
		hash := sha256.Sum256([]byte(input.Content))
		drawerID := fmt.Sprintf("drawer_%s_%s_%x", input.Wing, input.Room, hash[:8])

		// Classify memory type and derive importance.
		memoryType, importance := classifyAndScore(deps, input.Content)

		embedding := bestEffortEmbed(ctx, deps, input.Content)

		d := &memory.Drawer{
			ID:           drawerID,
			WorkspaceID:  workspaceID,
			Wing:         input.Wing,
			Room:         input.Room,
			Content:      input.Content,
			MemoryType:   memoryType,
			Importance:   importance,
			SourceFile:   input.SourceFile,
			AddedBy:      input.AddedBy,
			PipelineTier: memory.PipelineTierLLM,
			FiledAt:      time.Now().UTC(),
			CreatedAt:    time.Now().UTC(),
		}

		if err := deps.DrawerRepo.AddIdempotent(ctx, sess, d, embedding); err != nil {
			deps.Logger.ErrorContext(ctx, "failed to store drawer", "error", err)
			return nil, &mcpv1.MemoryAddDrawerOutput{Info: "failed to store memory"}, nil
		}

		// Reinforce similar memories.
		reinforceSimilar(ctx, sess, reinforceSimilarOpts{
			Deps:        deps,
			WorkspaceID: workspaceID,
			DrawerID:    drawerID,
			Embedding:   embedding,
		})

		return nil, &mcpv1.MemoryAddDrawerOutput{
			DrawerId:   drawerID,
			MemoryType: memoryType,
			Info:       "drawer added",
		}, nil
	})
}

// classifyAndScore returns the memory type and importance score for content.
// Falls back to "general" / defaultImportance when no classifier is configured.
func classifyAndScore(deps *Deps, content string) (memoryType string, importance float64) {
	memoryType = "general"
	importance = defaultImportance
	if deps.Classifier == nil {
		return memoryType, importance
	}
	classified := deps.Classifier.Classify(content, classifierMinConfidence)
	if len(classified) == 0 {
		return memoryType, importance
	}
	memoryType = classified[0].MemoryType
	for _, c := range classified {
		if c.Confidence*importanceScale > importance {
			importance = c.Confidence * importanceScale
		}
	}
	return memoryType, importance
}

// reinforceSimilarOpts groups the parameters for reinforceSimilar.
type reinforceSimilarOpts struct {
	Deps        *Deps
	WorkspaceID string
	DrawerID    string
	Embedding   []float32
}

// reinforceSimilar boosts the importance of existing drawers that are
// semantically similar to the newly-filed drawer. No-op when embedding is empty.
func reinforceSimilar(ctx context.Context, sess *dbr.Session, opts reinforceSimilarOpts) {
	if len(opts.Embedding) == 0 {
		return
	}
	similar, _ := opts.Deps.DrawerRepo.Search(ctx, sess, drawerrepo.SearchOpts{
		WorkspaceID:    opts.WorkspaceID,
		QueryEmbedding: opts.Embedding,
		Limit:          5,
	})
	for i := range similar {
		if similar[i].ID != opts.DrawerID && similar[i].Similarity > memory.ReinforcementThreshold {
			_ = opts.Deps.DrawerRepo.BoostImportance(ctx, sess, opts.WorkspaceID, similar[i].ID, memory.ReinforcementBoost, memory.MaxImportance)
		}
	}
}

func newMemoryDeleteDrawerHandler(deps *Deps) sdkmcp.ToolHandlerFor[memoryDeleteDrawerInput, *mcpv1.MemoryDeleteDrawerOutput] {
	return authedTool(deps, func(ctx context.Context, sess *dbr.Session, workspaceID string, input memoryDeleteDrawerInput) (*sdkmcp.CallToolResult, *mcpv1.MemoryDeleteDrawerOutput, error) {
		if input.DrawerID == "" {
			return nil, &mcpv1.MemoryDeleteDrawerOutput{Info: "drawer_id is required"}, nil
		}

		if err := deps.DrawerRepo.Delete(ctx, sess, workspaceID, input.DrawerID); err != nil {
			deps.Logger.ErrorContext(ctx, "failed to delete drawer", "error", err)
			return nil, &mcpv1.MemoryDeleteDrawerOutput{Info: "failed to delete drawer"}, nil
		}

		return nil, &mcpv1.MemoryDeleteDrawerOutput{Deleted: true, Info: "drawer deleted"}, nil
	})
}

func newMemorySetIdentityHandler(deps *Deps) sdkmcp.ToolHandlerFor[memorySetIdentityInput, *mcpv1.MemorySetIdentityOutput] {
	return authedTool(deps, func(ctx context.Context, sess *dbr.Session, workspaceID string, input memorySetIdentityInput) (*sdkmcp.CallToolResult, *mcpv1.MemorySetIdentityOutput, error) {
		if input.Content == "" {
			return nil, &mcpv1.MemorySetIdentityOutput{Info: "content is required"}, nil
		}
		if len(input.Content) > memory.MaxIdentityLength {
			return nil, &mcpv1.MemorySetIdentityOutput{Info: fmt.Sprintf("content exceeds max length of %d", memory.MaxIdentityLength)}, nil
		}

		if deps.IdentityRepo == nil {
			return nil, &mcpv1.MemorySetIdentityOutput{Info: "identity repo unavailable"}, nil
		}
		if err := deps.IdentityRepo.Set(ctx, sess, workspaceID, input.Content); err != nil {
			deps.Logger.ErrorContext(ctx, "failed to set identity", "error", err)
			return nil, &mcpv1.MemorySetIdentityOutput{Info: "failed to update identity"}, nil
		}

		return nil, &mcpv1.MemorySetIdentityOutput{Info: "identity set"}, nil
	})
}

// --- Knowledge Graph handlers ---

func newMemoryKGQueryHandler(deps *Deps) sdkmcp.ToolHandlerFor[memoryKGQueryInput, *mcpv1.MemoryKGQueryOutput] {
	return authedTool(deps, func(ctx context.Context, sess *dbr.Session, workspaceID string, input memoryKGQueryInput) (*sdkmcp.CallToolResult, *mcpv1.MemoryKGQueryOutput, error) {
		if input.Entity == "" {
			return nil, nil, fmt.Errorf("entity is required")
		}

		direction := input.Direction
		if direction == "" {
			direction = "both"
		}

		results, err := deps.KG.QueryEntity(ctx, sess, workspaceID, input.Entity, input.AsOf, direction)
		if err != nil {
			return nil, nil, err
		}

		out := tripleResultsToWire(results)
		return nil, &mcpv1.MemoryKGQueryOutput{Entity: input.Entity, Results: out, Count: int32(len(out))}, nil
	})
}

func newMemoryKGAddHandler(deps *Deps) sdkmcp.ToolHandlerFor[memoryKGAddInput, *mcpv1.MemoryKGAddOutput] {
	return authedTool(deps, func(ctx context.Context, sess *dbr.Session, workspaceID string, input memoryKGAddInput) (*sdkmcp.CallToolResult, *mcpv1.MemoryKGAddOutput, error) {
		if input.Subject == "" || input.Predicate == "" || input.Object == "" {
			return nil, &mcpv1.MemoryKGAddOutput{Info: "subject, predicate, and object are required"}, nil
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

		tripleID, err := deps.KG.AddTriple(ctx, sess, workspaceID, t)
		if err != nil {
			deps.Logger.ErrorContext(ctx, "failed to add triple", "error", err)
			return nil, &mcpv1.MemoryKGAddOutput{Info: "failed to update knowledge graph"}, nil
		}

		return nil, &mcpv1.MemoryKGAddOutput{TripleId: tripleID, Info: "triple added"}, nil
	})
}

func newMemoryKGInvalidateHandler(deps *Deps) sdkmcp.ToolHandlerFor[memoryKGInvalidateInput, *mcpv1.MemoryKGInvalidateOutput] {
	return authedTool(deps, func(ctx context.Context, sess *dbr.Session, workspaceID string, input memoryKGInvalidateInput) (*sdkmcp.CallToolResult, *mcpv1.MemoryKGInvalidateOutput, error) {
		if input.Subject == "" || input.Predicate == "" || input.Object == "" {
			return nil, &mcpv1.MemoryKGInvalidateOutput{Info: "subject, predicate, and object are required"}, nil
		}

		ended := input.Ended
		if ended == "" {
			ended = time.Now().Format("2006-01-02")
		}

		if err := deps.KG.Invalidate(ctx, sess, workspaceID, input.Subject, input.Predicate, input.Object, ended); err != nil {
			deps.Logger.ErrorContext(ctx, "failed to invalidate triple", "error", err)
			return nil, &mcpv1.MemoryKGInvalidateOutput{Info: "failed to update knowledge graph"}, nil
		}

		return nil, &mcpv1.MemoryKGInvalidateOutput{Info: "triple invalidated"}, nil
	})
}

func newMemoryKGTimelineHandler(deps *Deps) sdkmcp.ToolHandlerFor[memoryKGTimelineInput, *mcpv1.MemoryKGTimelineOutput] {
	return authedTool(deps, func(ctx context.Context, sess *dbr.Session, workspaceID string, input memoryKGTimelineInput) (*sdkmcp.CallToolResult, *mcpv1.MemoryKGTimelineOutput, error) {
		results, err := deps.KG.Timeline(ctx, sess, workspaceID, input.Entity)
		if err != nil {
			return nil, nil, err
		}

		out := tripleResultsToWire(results)
		return nil, &mcpv1.MemoryKGTimelineOutput{Events: out, Count: int32(len(out))}, nil
	})
}

func newMemoryKGStatsHandler(deps *Deps) sdkmcp.ToolHandlerFor[memoryKGStatsInput, *mcpv1.MemoryKGStatsOutput] {
	return authedTool(deps, func(ctx context.Context, sess *dbr.Session, workspaceID string, _ memoryKGStatsInput) (*sdkmcp.CallToolResult, *mcpv1.MemoryKGStatsOutput, error) {
		stats, err := deps.KG.Stats(ctx, sess, workspaceID)
		if err != nil {
			return nil, nil, err
		}

		return nil, &mcpv1.MemoryKGStatsOutput{
			Entities:          int32(stats.Entities),
			Triples:           int32(stats.Triples),
			CurrentFacts:      int32(stats.CurrentFacts),
			ExpiredFacts:      int32(stats.ExpiredFacts),
			RelationshipTypes: stats.RelationshipTypes,
		}, nil
	})
}

// --- Diary handlers ---

func newMemoryDiaryWriteHandler(deps *Deps) sdkmcp.ToolHandlerFor[memoryDiaryWriteInput, *mcpv1.MemoryDiaryWriteOutput] {
	return authedTool(deps, func(ctx context.Context, sess *dbr.Session, workspaceID string, input memoryDiaryWriteInput) (*sdkmcp.CallToolResult, *mcpv1.MemoryDiaryWriteOutput, error) {
		if input.AgentName == "" || input.Entry == "" {
			return nil, &mcpv1.MemoryDiaryWriteOutput{Info: "agent_name and entry are required"}, nil
		}
		if len(input.Entry) > memory.MaxContentLength {
			return nil, &mcpv1.MemoryDiaryWriteOutput{Info: fmt.Sprintf("entry exceeds max length of %d", memory.MaxContentLength)}, nil
		}

		agentName := strings.ToLower(strings.TrimSpace(input.AgentName))
		wing := agentWing(agentName)
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
			ID:           drawerID,
			WorkspaceID:  workspaceID,
			Wing:         wing,
			Room:         room,
			Hall:         input.Topic,
			Content:      input.Entry,
			MemoryType:   "diary",
			Importance:   defaultImportance,
			AddedBy:      input.AgentName,
			PipelineTier: memory.PipelineTierLLM,
			FiledAt:      time.Now().UTC(),
			CreatedAt:    time.Now().UTC(),
		}

		if err := deps.DrawerRepo.Add(ctx, sess, d, embedding); err != nil {
			deps.Logger.ErrorContext(ctx, "failed to write diary entry", "error", err)
			return nil, &mcpv1.MemoryDiaryWriteOutput{Info: "failed to write diary entry"}, nil
		}

		return nil, &mcpv1.MemoryDiaryWriteOutput{DrawerId: drawerID, Info: "diary entry written"}, nil
	})
}

func newMemoryDiaryReadHandler(deps *Deps) sdkmcp.ToolHandlerFor[memoryDiaryReadInput, *mcpv1.MemoryDiaryReadOutput] {
	return authedTool(deps, func(ctx context.Context, sess *dbr.Session, workspaceID string, input memoryDiaryReadInput) (*sdkmcp.CallToolResult, *mcpv1.MemoryDiaryReadOutput, error) {
		if input.AgentName == "" {
			return nil, nil, fmt.Errorf("agent_name is required")
		}

		lastN := input.LastN
		if lastN <= 0 {
			lastN = 10
		}
		if lastN > 100 {
			lastN = 100
		}

		agentName := strings.ToLower(strings.TrimSpace(input.AgentName))
		wing := agentWing(agentName)

		drawers, err := deps.DrawerRepo.GetByWingRoom(ctx, sess, workspaceID, wing, "diary", lastN)
		if err != nil {
			return nil, nil, err
		}

		entries := make([]*mcpv1.DiaryEntryBrief, 0, len(drawers))
		for i := range drawers {
			d := &drawers[i]
			entries = append(entries, &mcpv1.DiaryEntryBrief{
				Id:      d.ID,
				Content: d.Content,
				Hall:    d.Hall,
				FiledAt: d.FiledAt.Format(time.RFC3339),
			})
		}

		return nil, &mcpv1.MemoryDiaryReadOutput{Entries: entries, Count: int32(len(entries))}, nil
	})
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

// toMemorySearchResultBriefs maps repo-layer DrawerSearchResult values
// to the MCP wire shape. Used by both the memory_search and
// memory_check_duplicate tool handlers so the field set can never drift.
func toMemorySearchResultBriefs(results []memory.DrawerSearchResult) []*mcpv1.MemorySearchResultBrief {
	briefs := make([]*mcpv1.MemorySearchResultBrief, 0, len(results))
	for i := range results {
		r := &results[i]
		briefs = append(briefs, &mcpv1.MemorySearchResultBrief{
			Id:         r.ID,
			Wing:       r.Wing,
			Room:       r.Room,
			Content:    r.Content,
			MemoryType: r.MemoryType,
			Importance: r.Importance,
			Similarity: r.Similarity,
			FiledAt:    r.FiledAt.Format(time.RFC3339),
		})
	}
	return briefs
}

// tripleResultsToWire maps repo-layer TripleResult values to the MCP
// wire shape. Used by kg_query and kg_timeline handlers.
func tripleResultsToWire(results []memory.TripleResult) []*mcpv1.TripleResultOut {
	out := make([]*mcpv1.TripleResultOut, 0, len(results))
	for i := range results {
		r := &results[i]
		tr := &mcpv1.TripleResultOut{
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
	return out
}

// toProtoWingCounts converts repo-layer WingCount values to proto pointers.
func toProtoWingCounts(wings []memory.WingCount) []*mcpv1.MemoryWingCount {
	out := make([]*mcpv1.MemoryWingCount, 0, len(wings))
	for _, w := range wings {
		out = append(out, &mcpv1.MemoryWingCount{Wing: w.Wing, Count: int32(w.Count)})
	}
	return out
}

// toProtoRoomCounts converts repo-layer RoomCount values to proto pointers.
func toProtoRoomCounts(rooms []memory.RoomCount) []*mcpv1.MemoryRoomCount {
	out := make([]*mcpv1.MemoryRoomCount, 0, len(rooms))
	for _, r := range rooms {
		out = append(out, &mcpv1.MemoryRoomCount{Wing: r.Wing, Room: r.Room, Count: int32(r.Count)})
	}
	return out
}

// agentWing returns the canonical drawer-wing name for an agent. Agent
// names are user-supplied and may contain spaces or apostrophes; the
// slug form is lowercase with spaces replaced by underscores and
// apostrophes stripped.
func agentWing(agentName string) string {
	slug := strings.ToLower(agentName)
	slug = strings.ReplaceAll(slug, " ", "_")
	slug = strings.ReplaceAll(slug, "'", "")
	return "wing_" + slug
}
