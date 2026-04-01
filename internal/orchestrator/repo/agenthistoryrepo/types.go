package agenthistoryrepo

type agentHistoryRepo struct{}

var historyColumns = []string{
	"id",
	"agent_id",
	"conversation_id",
	"title",
	"subtitle",
	"created_at",
}
