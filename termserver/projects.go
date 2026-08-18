package termserver

import (
	"net/http"

	"github.com/DiegoAvila-yeyo/exo/projects"
)

// projectScanner is the subset of the projects package the server needs —
// same decoupling pattern as chatSessionStore.
type projectScanner func(root string) ([]projects.Project, error)

// handleProjects serves the composer's "select project" picker: a live scan
// of s.projectRoot, most recently modified first. No caching — a folder
// created a second ago shows up on the next click, no registration step.
func (s *Server) handleProjects(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if !ValidReadOrigin(r.Header.Get("Origin"), s.port) {
		http.Error(w, "forbidden origin", http.StatusForbidden)
		return
	}
	if s.projectRoot == "" || s.scanProjects == nil {
		writeJSON(w, http.StatusOK, []projects.Project{})
		return
	}

	list, err := s.scanProjects(s.projectRoot)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	s.recordActivity()
	writeJSON(w, http.StatusOK, list)
}
