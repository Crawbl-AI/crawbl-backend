-- MemPalace memory system: vector store, knowledge graph, identities.
-- Requires pgvector extension for semantic search.

CREATE EXTENSION IF NOT EXISTS vector;

-- Core vector store — each drawer is a chunk of verbatim content with embedding.
CREATE TABLE memory_drawers (
    id            TEXT        PRIMARY KEY,
    workspace_id  UUID        NOT NULL,
    wing          TEXT        NOT NULL,
    room          TEXT        NOT NULL,
    hall          TEXT        NOT NULL DEFAULT '',
    content       TEXT        NOT NULL,
    embedding     vector(1536),
    importance    REAL        NOT NULL DEFAULT 3.0,
    memory_type   TEXT        NOT NULL DEFAULT '',
    source_file   TEXT        NOT NULL DEFAULT '',
    added_by      TEXT        NOT NULL DEFAULT 'system',
    filed_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    created_at    TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX idx_drawers_workspace      ON memory_drawers (workspace_id);
CREATE INDEX idx_drawers_workspace_wing ON memory_drawers (workspace_id, wing);
CREATE INDEX idx_drawers_workspace_room ON memory_drawers (workspace_id, wing, room);
CREATE INDEX idx_drawers_embedding      ON memory_drawers
    USING hnsw (embedding vector_cosine_ops) WITH (m = 16, ef_construction = 64);

-- Knowledge graph: entity nodes.
CREATE TABLE memory_entities (
    id            TEXT        NOT NULL,
    workspace_id  UUID        NOT NULL,
    name          TEXT        NOT NULL,
    type          TEXT        NOT NULL DEFAULT 'unknown',
    properties    JSONB       NOT NULL DEFAULT '{}',
    created_at    TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (workspace_id, id)
);

-- Knowledge graph: temporal relationship triples.
CREATE TABLE memory_triples (
    id              TEXT        NOT NULL,
    workspace_id    UUID        NOT NULL,
    subject         TEXT        NOT NULL,
    predicate       TEXT        NOT NULL,
    object          TEXT        NOT NULL,
    valid_from      TEXT,
    valid_to        TEXT,
    confidence      REAL        NOT NULL DEFAULT 1.0,
    source_closet   TEXT        NOT NULL DEFAULT '',
    source_file     TEXT        NOT NULL DEFAULT '',
    extracted_at    TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (workspace_id, id)
);

CREATE INDEX idx_triples_subject   ON memory_triples (workspace_id, subject);
CREATE INDEX idx_triples_object    ON memory_triples (workspace_id, object);
CREATE INDEX idx_triples_predicate ON memory_triples (workspace_id, predicate);
CREATE INDEX idx_triples_validity  ON memory_triples (workspace_id, valid_from, valid_to);

-- Per-workspace L0 identity text.
CREATE TABLE memory_identities (
    workspace_id  UUID        PRIMARY KEY,
    content       TEXT        NOT NULL DEFAULT '',
    updated_at    TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
