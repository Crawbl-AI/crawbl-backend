-- ClickHouse DDL for LLM usage analytics.
-- Applied manually or by the usage-writer on first startup.

CREATE TABLE IF NOT EXISTS llm_usage (
    event_id UUID DEFAULT generateUUIDv4(),
    event_time DateTime64(3, 'UTC') DEFAULT now64(3),
    user_id UUID,
    workspace_id UUID,
    conversation_id UUID,
    message_id String DEFAULT '',
    agent_id String,
    agent_db_id UUID DEFAULT '00000000-0000-0000-0000-000000000000',
    model String,
    provider String DEFAULT 'openai',
    prompt_tokens Int32 DEFAULT 0,
    completion_tokens Int32 DEFAULT 0,
    total_tokens Int32 DEFAULT 0,
    tool_use_prompt_tokens Int32 DEFAULT 0,
    thoughts_tokens Int32 DEFAULT 0,
    cached_tokens Int32 DEFAULT 0,
    cost_usd Decimal64(6) DEFAULT 0,
    call_sequence Int32 DEFAULT 0,
    turn_id String DEFAULT '',
    session_id String DEFAULT ''
) ENGINE = MergeTree()
PARTITION BY toYYYYMM(event_time)
ORDER BY (user_id, workspace_id, event_time, event_id)
TTL toDateTime(event_time) + INTERVAL 13 MONTH
SETTINGS index_granularity = 8192;

-- Daily rollup materialized view for dashboard queries.
CREATE TABLE IF NOT EXISTS llm_usage_daily (
    day Date,
    user_id UUID,
    workspace_id UUID,
    model String,
    agent_id String,
    total_prompt_tokens Int64,
    total_completion_tokens Int64,
    total_tokens Int64,
    total_cost_usd Decimal128(6),
    request_count Int64
) ENGINE = SummingMergeTree()
PARTITION BY toYYYYMM(day)
ORDER BY (user_id, workspace_id, day, model, agent_id)
TTL day + INTERVAL 25 MONTH;

CREATE MATERIALIZED VIEW IF NOT EXISTS llm_usage_daily_mv
TO llm_usage_daily AS
SELECT
    toDate(event_time) AS day,
    user_id,
    workspace_id,
    model,
    agent_id,
    sum(prompt_tokens) AS total_prompt_tokens,
    sum(completion_tokens) AS total_completion_tokens,
    sum(total_tokens) AS total_tokens,
    sum(cost_usd) AS total_cost_usd,
    count() AS request_count
FROM llm_usage
GROUP BY day, user_id, workspace_id, model, agent_id;
