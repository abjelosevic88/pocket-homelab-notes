package server

import "net/http"

// A line of a note sent away: a Nextcloud task or Deck card, a Pocket Money
// transaction. Stub until built.

func (s *Server) handleToolTaskTargets(w http.ResponseWriter, r *http.Request) {
	writeErr(w, http.StatusNotImplemented, "not built yet")
}

func (s *Server) handleToolTask(w http.ResponseWriter, r *http.Request) {
	writeErr(w, http.StatusNotImplemented, "not built yet")
}

func (s *Server) handleToolExpenseOptions(w http.ResponseWriter, r *http.Request) {
	writeErr(w, http.StatusNotImplemented, "not built yet")
}

func (s *Server) handleToolExpense(w http.ResponseWriter, r *http.Request) {
	writeErr(w, http.StatusNotImplemented, "not built yet")
}
