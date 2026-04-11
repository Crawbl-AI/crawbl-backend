package agentsettingsrepo

type agentSettingsRepo struct{}

var settingsColumns = []any{
	"agent_id",
	"model",
	"response_length",
	"allowed_tools",
	"created_at",
	"updated_at",
}
