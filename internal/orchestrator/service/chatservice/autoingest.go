package chatservice

import (
	"context"
	"crypto/md5"
	"fmt"
	"log/slog"
	"regexp"
	"strings"
	"time"

	"github.com/Crawbl-AI/crawbl-backend/internal/memory"
	"github.com/Crawbl-AI/crawbl-backend/internal/memory/background"
	"github.com/Crawbl-AI/crawbl-backend/internal/memory/config"
	orchestrator "github.com/Crawbl-AI/crawbl-backend/internal/orchestrator"
	"github.com/Crawbl-AI/crawbl-backend/internal/orchestrator/queue"
)

const importanceScale = 5.0

var noiseConfig = mustLoadNoiseConfig()
var noisePattern = noiseConfig.CompileNoisePattern()

func mustLoadNoiseConfig() *config.NoiseConfig {
	cfg, err := config.LoadNoiseConfig()
	if err != nil {
		panic(fmt.Sprintf("failed to load noise config: %v", err))
	}
	return cfg
}

// sentenceBoundary splits text on sentence-ending punctuation followed by whitespace.
var sentenceBoundary = regexp.MustCompile(`([.!?])\s+`)

// isNoise returns true if text is too short or is a greeting/filler message.
func isNoise(text string) bool {
	if len(text) < noiseConfig.MinLength {
		return true
	}
	return noisePattern.MatchString(strings.TrimSpace(text))
}

// buildExchange constructs a paired user/agent exchange string from a user
// message and agent replies, skipping empty or delegation-type replies.
func buildExchange(userText string, replies []*orchestrator.Message) string {
	var b strings.Builder
	b.WriteString("User: ")
	b.WriteString(userText)

	for _, reply := range replies {
		if reply.Content.Text == "" {
			continue
		}
		if reply.Content.Type == orchestrator.MessageContentTypeDelegation {
			continue
		}
		b.WriteString("\n\nAgent: ")
		b.WriteString(reply.Content.Text)
	}

	return b.String()
}

// chunkText splits text on sentence boundaries into chunks of at most maxSize
// characters, with overlap from the previous chunk's tail. Chunks smaller than
// minChunk are discarded.
func chunkText(text string, maxSize, overlap, minChunk int) []string {
	if len(text) <= maxSize {
		return []string{text}
	}

	// Split on sentence boundaries, keeping the punctuation with the preceding segment.
	parts := sentenceBoundary.Split(text, -1)
	puncts := sentenceBoundary.FindAllStringSubmatch(text, -1)

	// Reassemble sentences with their trailing punctuation.
	sentences := make([]string, 0, len(parts))
	for i, part := range parts {
		if i < len(puncts) {
			sentences = append(sentences, part+puncts[i][1])
		} else {
			sentences = append(sentences, part)
		}
	}

	var chunks []string
	var current strings.Builder
	var overlapText string

	for _, sentence := range sentences {
		// If adding this sentence would exceed maxSize, finalize the current chunk.
		if current.Len() > 0 && current.Len()+1+len(sentence) > maxSize {
			chunk := current.String()
			if len(chunk) >= minChunk {
				chunks = append(chunks, chunk)
			}
			// Compute overlap from the tail of the current chunk.
			if overlap > 0 && len(chunk) > overlap {
				overlapText = chunk[len(chunk)-overlap:]
			} else {
				overlapText = chunk
			}
			current.Reset()
			current.WriteString(overlapText)
		}

		if current.Len() > 0 {
			current.WriteString(" ")
		}
		current.WriteString(sentence)
	}

	// Flush remaining content.
	if current.Len() >= minChunk {
		chunks = append(chunks, current.String())
	}

	// If no sentence boundary was found, hard-split at maxSize.
	if len(chunks) == 0 {
		for i := 0; i < len(text); i += maxSize - overlap {
			end := i + maxSize
			if end > len(text) {
				end = len(text)
			}
			chunk := text[i:end]
			if len(chunk) >= minChunk {
				chunks = append(chunks, chunk)
			}
			if end == len(text) {
				break
			}
		}
	}

	return chunks
}

// autoIngestDrawerID generates a deterministic MD5-based drawer ID from the
// room and content to ensure idempotent inserts.
// The wing is always memory.AutoIngestWing for auto-ingested conversations.
func autoIngestDrawerID(room, content string) string {
	hash := md5.Sum([]byte(content))
	return fmt.Sprintf("drawer_%s_%s_%x", memory.AutoIngestWing, room, hash[:8])
}

// StartIngestWorker starts the single background goroutine that drains the
// ingest queue and writes memory drawers. It is a no-op when the drawer repo
// is not configured.
func (s *service) StartIngestWorker(ctx context.Context) {
	if s.drawerRepo == nil {
		return
	}
	go s.runIngestWorker(ctx)
}

// runIngestWorker is the main loop that processes ingest work items until the
// context is cancelled.
func (s *service) runIngestWorker(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return
		case item := <-s.ingestQueue:
			func() {
				defer func() {
					if r := recover(); r != nil {
						slog.Error("auto-ingest: worker panic recovered",
							slog.Any("panic", r),
							slog.String("workspace_id", item.workspaceID),
							slog.String("agent", item.agentSlug),
						)
					}
				}()
				s.processIngestWork(item)
			}()
		}
	}
}

// processIngestWork handles a single ingest work item: builds the exchange,
// chunks it, classifies each chunk, deduplicates via embedding similarity,
// and persists new drawers.
func (s *service) processIngestWork(w ingestWork) {
	start := time.Now()

	ctx, cancel := context.WithTimeout(context.Background(), time.Duration(memory.AutoIngestTimeout)*time.Second)
	defer cancel()

	sess := s.db.NewSession(nil)

	exchange := buildExchange(w.userText, w.replies)
	if isNoise(exchange) {
		return
	}

	chunks := chunkText(exchange, memory.AutoIngestChunkSize, memory.AutoIngestChunkOverlap, memory.AutoIngestMinChunk)

	var ingested, skippedDedup int

	for _, chunk := range chunks {
		memType := ""
		importance := memory.DefaultImportance

		// Heuristic classification.
		if s.classifier != nil {
			classified := s.classifier.Classify(chunk, memory.AutoIngestMinConfidence)
			if len(classified) > 0 {
				memType = classified[0].MemoryType
				importance = classified[0].Confidence * importanceScale
			}
		}

		room := memory.MemoryTypeToRoom(memType)

		// Embed the chunk.
		var embedding []float32
		if s.embedder != nil {
			var err error
			embedding, err = s.embedder.Embed(ctx, chunk)
			if err != nil {
				slog.Warn("auto-ingest: embedding failed",
					slog.String("workspace_id", w.workspaceID),
					slog.String("error", err.Error()),
				)
			}
		}

		// Deduplication via cosine similarity.
		if embedding != nil {
			dupes, _ := s.drawerRepo.CheckDuplicate(ctx, sess, w.workspaceID, embedding, memory.AutoIngestDupThreshold, 1)
			if len(dupes) > 0 {
				skippedDedup++
				continue
			}
		}

		now := time.Now().UTC()
		d := &memory.Drawer{
			ID:           autoIngestDrawerID(room, chunk),
			WorkspaceID:  w.workspaceID,
			Wing:         memory.AutoIngestWing,
			Room:         room,
			Content:      chunk,
			Importance:   importance,
			MemoryType:   memType,
			AddedBy:      memory.AutoIngestAddedBy,
			AddedByAgent: w.agentSlug,
			State:        string(memory.DrawerStateRaw),
			FiledAt:      now,
			CreatedAt:    now,
		}

		if err := s.drawerRepo.AddIdempotent(ctx, sess, d, embedding); err != nil {
			slog.Warn("auto-ingest: drawer insert failed",
				slog.String("workspace_id", w.workspaceID),
				slog.String("drawer_id", d.ID),
				slog.String("error", err.Error()),
			)
			continue
		}

		ingested++

		if s.memoryPublisher != nil {
			s.memoryPublisher.Publish(ctx, w.workspaceID, &queue.MemoryEvent{
				WorkspaceID: w.workspaceID,
				DrawerID:    d.ID,
				Wing:        d.Wing,
				Room:        d.Room,
				MemoryType:  d.MemoryType,
				AgentID:     w.agentSlug,
				ContentLen:  len(chunk),
			})
		}

		// Enqueue an ad-hoc process job so the cold pipeline runs within ~100ms
		// instead of waiting up to a minute for the periodic River sweep.
		// Safety-net sweep still runs every minute — UniqueOpts dedupes overlap.
		// Non-transactional on purpose: if this Insert fails the drawer survives
		// as `raw` and the periodic sweep picks it up.
		if s.riverClient != nil {
			if _, enqErr := s.riverClient.Insert(ctx, background.ProcessArgs{}, nil); enqErr != nil {
				slog.Warn("auto-ingest: river enqueue failed",
					slog.String("workspace_id", w.workspaceID),
					slog.String("drawer_id", d.ID),
					slog.String("error", enqErr.Error()),
				)
			}
		}
	}

	slog.Info("auto-ingest complete",
		slog.String("workspace_id", w.workspaceID),
		slog.String("agent", w.agentSlug),
		slog.Int("ingested", ingested),
		slog.Int("skipped_dedup", skippedDedup),
		slog.Duration("duration", time.Since(start)),
	)
}

// autoIngestConversation enqueues a conversation exchange for background
// memory ingestion. Silently drops the work item if the queue is full.
func (s *service) autoIngestConversation(workspaceID, agentSlug, userText string, replies []*orchestrator.Message) {
	if s.drawerRepo == nil {
		return
	}
	select {
	case s.ingestQueue <- ingestWork{
		workspaceID: workspaceID,
		agentSlug:   agentSlug,
		userText:    userText,
		replies:     replies,
	}:
	default:
		slog.Warn("auto-ingest: queue full, dropping",
			slog.String("workspace_id", workspaceID),
		)
	}
}
