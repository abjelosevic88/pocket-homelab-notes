package server

import "net/http"

// Blocks written from what the services know. Stub until built.

func (s *Server) handleToolReport(w http.ResponseWriter, r *http.Request) {
	writeErr(w, http.StatusNotImplemented, "not built yet")
}
