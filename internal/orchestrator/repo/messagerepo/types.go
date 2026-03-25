package messagerepo

const defaultListLimit = 50

var messageColumns = []string{
	"id",
	"conversation_id",
	"role",
	"content",
	"status",
	"local_id",
	"agent_id",
	"attachments",
	"created_at",
	"updated_at",
}
