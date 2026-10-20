package server

import "net/http"

// Pictures from Immich and files from Nextcloud, into a note. Stub until built.

func (s *Server) handleToolPhotos(w http.ResponseWriter, r *http.Request) {
	writeErr(w, http.StatusNotImplemented, "not built yet")
}

func (s *Server) handleToolPhotoThumb(w http.ResponseWriter, r *http.Request) {
	writeErr(w, http.StatusNotImplemented, "not built yet")
}

func (s *Server) handleToolPhotosAttach(w http.ResponseWriter, r *http.Request) {
	writeErr(w, http.StatusNotImplemented, "not built yet")
}

func (s *Server) handleToolFiles(w http.ResponseWriter, r *http.Request) {
	writeErr(w, http.StatusNotImplemented, "not built yet")
}

func (s *Server) handleToolFilesAttach(w http.ResponseWriter, r *http.Request) {
	writeErr(w, http.StatusNotImplemented, "not built yet")
}
