package webui

import (
	"net/http"
)

type sessionRow struct {
	SessionID           string `json:"session_id"`
	Node                string `json:"node"`
	Region              string `json:"region"`
	RemainingTTLSeconds int64  `json:"remaining_ttl_seconds"`
}

func (s *Server) apiSessions(w http.ResponseWriter, _ *http.Request) {
	bindings := s.affinity.List()
	rows := make([]sessionRow, 0, len(bindings))
	for _, binding := range bindings {
		rows = append(rows, sessionRow{
			SessionID:           binding.SessionID,
			Node:                binding.NodeAddress,
			Region:              binding.Region,
			RemainingTTLSeconds: int64(s.affinity.RemainingTTL(binding).Seconds()),
		})
	}
	jsonOK(w, rows)
}
