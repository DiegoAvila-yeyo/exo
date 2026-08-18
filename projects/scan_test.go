package projects

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestScanReturnsOnlyDirsWithMarkerFiles(t *testing.T) {
	root := t.TempDir()

	mkProjectDir(t, root, "has-git", ".git")
	mkProjectDir(t, root, "has-go-mod", "go.mod")
	mkPlainDir(t, root, "no-marker")
	mkPlainDir(t, root, ".hidden-with-git", ".git") // hidden top-level, must be skipped

	projects, err := Scan(root)
	if err != nil {
		t.Fatalf("scan failed: %v", err)
	}

	names := make(map[string]bool)
	for _, p := range projects {
		names[p.Name] = true
	}
	if !names["has-git"] || !names["has-go-mod"] {
		t.Fatalf("projects = %+v, want has-git and has-go-mod present", projects)
	}
	if names["no-marker"] {
		t.Fatalf("projects = %+v, want no-marker excluded (no marker file)", projects)
	}
	if names[".hidden-with-git"] {
		t.Fatalf("projects = %+v, want .hidden-with-git excluded (hidden top-level dir)", projects)
	}
}

func TestScanOrdersMostRecentlyModifiedFirst(t *testing.T) {
	root := t.TempDir()

	mkProjectDir(t, root, "older", ".git")
	time.Sleep(20 * time.Millisecond)
	mkProjectDir(t, root, "newer", ".git")

	projects, err := Scan(root)
	if err != nil {
		t.Fatalf("scan failed: %v", err)
	}
	if len(projects) != 2 {
		t.Fatalf("projects = %+v, want 2 entries", projects)
	}
	if projects[0].Name != "newer" {
		t.Fatalf("projects[0].Name = %q, want %q (most recently modified first)", projects[0].Name, "newer")
	}
}

func TestScanReturnsAbsolutePath(t *testing.T) {
	root := t.TempDir()
	mkProjectDir(t, root, "proj", ".git")

	projects, err := Scan(root)
	if err != nil {
		t.Fatalf("scan failed: %v", err)
	}
	if len(projects) != 1 {
		t.Fatalf("projects = %+v, want 1 entry", projects)
	}
	want := filepath.Join(root, "proj")
	if projects[0].Path != want {
		t.Fatalf("path = %q, want %q", projects[0].Path, want)
	}
}

func mkProjectDir(t *testing.T, root, name, marker string) {
	t.Helper()
	dir := filepath.Join(root, name)
	if err := os.Mkdir(dir, 0o755); err != nil {
		t.Fatalf("mkdir %q failed: %v", dir, err)
	}
	markerPath := filepath.Join(dir, marker)
	if marker == ".git" {
		if err := os.Mkdir(markerPath, 0o755); err != nil {
			t.Fatalf("mkdir %q failed: %v", markerPath, err)
		}
		return
	}
	if err := os.WriteFile(markerPath, []byte("{}"), 0o644); err != nil {
		t.Fatalf("write %q failed: %v", markerPath, err)
	}
}

func mkPlainDir(t *testing.T, root, name string, marker ...string) {
	t.Helper()
	dir := filepath.Join(root, name)
	if err := os.Mkdir(dir, 0o755); err != nil {
		t.Fatalf("mkdir %q failed: %v", dir, err)
	}
	if len(marker) == 1 {
		if err := os.Mkdir(filepath.Join(dir, marker[0]), 0o755); err != nil {
			t.Fatalf("mkdir marker failed: %v", err)
		}
	}
}
