// Package projects discovers real project folders under a root directory
// (typically $HOME) for the "select project" picker in the chat UI. There is
// no registry to consult — this scans the filesystem live, so a newly
// created folder shows up the next time the list is requested, with no
// registration step.
package projects

import (
	"os"
	"path/filepath"
	"sort"
)

// Project is one entry in the picker: a folder name and its absolute path.
type Project struct {
	Name string `json:"name"`
	Path string `json:"path"`
}

// markerFiles are the files/dirs whose presence marks a folder as a real
// project rather than an unrelated top-level folder (Desktop, Downloads, ...).
// Kept short and easy to extend as new ecosystems come up.
var markerFiles = []string{
	".git",
	"go.mod",
	"package.json",
	"Cargo.toml",
	"pyproject.toml",
	"requirements.txt",
}

// Scan lists first-level directories under root that look like real
// projects (contain one of markerFiles), most recently modified first.
// Hidden directories (dotfiles) are skipped outright — a project's own
// .git doesn't make the project itself hidden, but the top-level entry
// being named ".foo" does.
func Scan(root string) ([]Project, error) {
	entries, err := os.ReadDir(root)
	if err != nil {
		return nil, err
	}

	type candidate struct {
		Project
		modTime int64
	}
	var candidates []candidate

	for _, entry := range entries {
		if !entry.IsDir() || entry.Name()[0] == '.' {
			continue
		}
		path := filepath.Join(root, entry.Name())
		if !hasMarker(path) {
			continue
		}
		info, err := entry.Info()
		if err != nil {
			continue
		}
		candidates = append(candidates, candidate{
			Project: Project{Name: entry.Name(), Path: path},
			modTime: info.ModTime().Unix(),
		})
	}

	sort.Slice(candidates, func(i, j int) bool {
		return candidates[i].modTime > candidates[j].modTime
	})

	out := make([]Project, len(candidates))
	for i, c := range candidates {
		out[i] = c.Project
	}
	return out, nil
}

func hasMarker(dir string) bool {
	for _, marker := range markerFiles {
		if _, err := os.Stat(filepath.Join(dir, marker)); err == nil {
			return true
		}
	}
	return false
}
