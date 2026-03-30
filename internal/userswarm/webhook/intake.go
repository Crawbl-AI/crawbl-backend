package webhook

import (
	"encoding/json"
	"net/http"

	crawblv1alpha1 "github.com/Crawbl-AI/crawbl-backend/api/v1alpha1"
)

// syncSurface owns the wire format of the Metacontroller callback.
// It is intentionally thin: decode request, decode parent, hand control to the
// reconciliation flow, then encode the response back to JSON.
func syncSurface(cfg *runtimeConfig) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		req, err := decodeSyncRequest(r)
		if err != nil {
			http.Error(w, "invalid request: "+err.Error(), http.StatusBadRequest)
			return
		}

		swarm, err := decodeParentSwarm(req.Parent)
		if err != nil {
			http.Error(w, "invalid parent: "+err.Error(), http.StatusBadRequest)
			return
		}

		resp := driveSync(req, swarm, cfg)
		if err := writeSyncResponse(w, resp); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
		}
	}
}

func decodeSyncRequest(r *http.Request) (*syncRequest, error) {
	defer r.Body.Close()

	var req syncRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		return nil, err
	}
	return &req, nil
}

func decodeParentSwarm(raw json.RawMessage) (*crawblv1alpha1.UserSwarm, error) {
	var swarm crawblv1alpha1.UserSwarm
	if err := json.Unmarshal(raw, &swarm); err != nil {
		return nil, err
	}
	return &swarm, nil
}

func writeSyncResponse(w http.ResponseWriter, resp *syncResponse) error {
	data, err := json.Marshal(resp)
	if err != nil {
		return err
	}

	w.Header().Set("Content-Type", "application/json")
	_, err = w.Write(data)
	return err
}
