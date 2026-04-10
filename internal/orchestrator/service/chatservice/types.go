// Package chatservice provides the chat service implementation for handling
// agent listings, conversations, and message operations within user workspaces.
// It orchestrates workspace bootstrapping, default agent provisioning, and
// runtime communication for swarm-based chat interactions.
package chatservice

import (
	"github.com/gocraft/dbr/v2"

	"github.com/Crawbl-AI/crawbl-backend/internal/memory/drawer"
	"github.com/Crawbl-AI/crawbl-backend/internal/memory/extract"
	"github.com/Crawbl-AI/crawbl-backend/internal/memory/kg"
	"github.com/Crawbl-AI/crawbl-backend/internal/memory/layers"
	orchestrator "github.com/Crawbl-AI/crawbl-backend/internal/orchestrator"
	"github.com/Crawbl-AI/crawbl-backend/internal/orchestrator/queue"
	orchestratorrepo "github.com/Crawbl-AI/crawbl-backend/internal/orchestrator/repo"
	"github.com/Crawbl-AI/crawbl-backend/internal/orchestrator/repo/usagerepo"
	"github.com/Crawbl-AI/crawbl-backend/internal/pkg/embed"
	"github.com/Crawbl-AI/crawbl-backend/internal/pkg/pricing"
	"github.com/Crawbl-AI/crawbl-backend/internal/pkg/realtime"
	pkgriver "github.com/Crawbl-AI/crawbl-backend/internal/pkg/river"
	userswarmclient "github.com/Crawbl-AI/crawbl-backend/internal/userswarm/client"
)

// Repos groups the repository dependencies used by the chat service.
// Passing a single struct instead of 8 individual parameters keeps the
// constructor signature clean and makes adding new repos a one-line change.
type Repos struct {
	Workspace     orchestratorrepo.WorkspaceRepo
	Agent         orchestratorrepo.AgentRepo
	Conversation  orchestratorrepo.ConversationRepo
	Message       orchestratorrepo.MessageRepo
	Tools         orchestratorrepo.ToolsRepo
	AgentSettings orchestratorrepo.AgentSettingsRepo
	AgentPrompts  orchestratorrepo.AgentPromptsRepo
	AgentHistory  orchestratorrepo.AgentHistoryRepo
	Usage         usagerepo.Repo
}

// MemoryDeps groups the memory pipeline dependencies.
// All fields are optional — when nil, the corresponding feature is disabled.
// RiverClient is optional; when non-nil, auto-ingest can enqueue ad-hoc
// memory_process jobs for near-realtime processing (Phase 4). When nil the
// periodic River sweep still runs — callers remain fully functional.
type MemoryDeps struct {
	DrawerRepo    drawer.Repo
	Classifier    extract.Classifier
	LLMClassifier extract.LLMClassifier
	Embedder      embed.Embedder
	KGGraph       kg.Graph
	// RiverClient is the in-process River job queue client. Optional — nil is
	// safe everywhere; Phase 4 will use it to enqueue ad-hoc process jobs from
	// auto-ingest immediately after a raw drawer is written.
	RiverClient *pkgriver.Client
}

// service implements the ChatService interface.
type service struct {
	db                *dbr.Connection
	workspaceRepo     orchestratorrepo.WorkspaceRepo
	agentRepo         orchestratorrepo.AgentRepo
	conversationRepo  orchestratorrepo.ConversationRepo
	messageRepo       orchestratorrepo.MessageRepo
	toolsRepo         orchestratorrepo.ToolsRepo
	agentSettingsRepo orchestratorrepo.AgentSettingsRepo
	agentPromptsRepo  orchestratorrepo.AgentPromptsRepo
	agentHistoryRepo  orchestratorrepo.AgentHistoryRepo
	usageRepo         usagerepo.Repo
	runtimeClient     userswarmclient.Client
	broadcaster       realtime.Broadcaster
	defaultAgents     []orchestrator.DefaultAgentBlueprint
	memoryStack       layers.Stack
	pricingCache      *pricing.Cache
	usagePublisher    *queue.UsagePublisher
	memoryPublisher   *queue.MemoryPublisher
	// Memory pipeline dependencies.
	drawerRepo    drawer.Repo
	classifier    extract.Classifier
	llmClassifier extract.LLMClassifier
	embedder      embed.Embedder
	kgGraph       kg.Graph
	// riverClient is the River job queue client for enqueuing ad-hoc
	// memory_process jobs. Nil when not wired (safe — periodic sweep covers it).
	riverClient *pkgriver.Client
	ingestQueue chan ingestWork
}

// ingestWork represents a unit of work for the memory auto-ingest pipeline.
type ingestWork struct {
	workspaceID string
	agentSlug   string
	userText    string
	replies     []*orchestrator.Message
}
