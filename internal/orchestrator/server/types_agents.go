package server

type agentResponse struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	Role      string `json:"role"`
	Avatar    string `json:"avatar"`
	Status    string `json:"status"`
	HasUpdate bool   `json:"hasUpdate"`
}
