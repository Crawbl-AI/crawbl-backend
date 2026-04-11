package agenthistoryrepo

type agentHistoryRepo struct{}

var historyColumns = []any{
	"id",
	"agent_id",
	"conversation_id",
	"title",
	"subtitle",
	"created_at",
}
