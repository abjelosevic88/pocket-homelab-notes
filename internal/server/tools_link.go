package server

import "net/http"

// Links to what lives elsewhere (Nextcloud files, Paperless documents, Forgejo
// issues, Jellyfin titles, Immich photos) and web bookmarks. Stub until built.

func (s *Server) handleToolLink(w http.ResponseWriter, r *http.Request) {
	writeErr(w, http.StatusNotImplemented, "not built yet")
}

func (s *Server) handleToolBookmark(w http.ResponseWriter, r *http.Request) {
	writeErr(w, http.StatusNotImplemented, "not built yet")
}
