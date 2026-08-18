package termserver

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/DiegoAvila-yeyo/exo/projects"
)

func TestProjectsReturnsScannedList(t *testing.T) {
	store := newFakeStore()
	fakeScan := func(root string) ([]projects.Project, error) {
		if root != "/fake/home" {
			t.Fatalf("scan called with root = %q, want /fake/home", root)
		}
		return []projects.Project{
			{Name: "exo", Path: "/fake/home/exo"},
			{Name: "avengers", Path: "/fake/home/avengers"},
		}, nil
	}
	_, server, httpServer := newTestServer(t, store, WithProjectScanner("/fake/home", fakeScan))
	client, _ := bootstrapClient(t, httpServer, server)

	req, err := http.NewRequest(http.MethodGet, httpServer.URL+"/api/projects", nil)
	if err != nil {
		t.Fatalf("build request failed: %v", err)
	}
	req.Header.Set("Origin", allowedOrigin(server))
	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("GET /api/projects failed: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want %d", resp.StatusCode, http.StatusOK)
	}

	var got []projects.Project
	if err := json.NewDecoder(resp.Body).Decode(&got); err != nil {
		t.Fatalf("decode failed: %v", err)
	}
	if len(got) != 2 || got[0].Name != "exo" {
		t.Fatalf("projects = %+v, want [exo, avengers]", got)
	}
}

func TestProjectsReturnsEmptyListWhenNotConfigured(t *testing.T) {
	store := newFakeStore()
	_, server, httpServer := newTestServer(t, store)
	client, _ := bootstrapClient(t, httpServer, server)

	req, err := http.NewRequest(http.MethodGet, httpServer.URL+"/api/projects", nil)
	if err != nil {
		t.Fatalf("build request failed: %v", err)
	}
	req.Header.Set("Origin", allowedOrigin(server))
	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("GET /api/projects failed: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want %d", resp.StatusCode, http.StatusOK)
	}
	var got []projects.Project
	if err := json.NewDecoder(resp.Body).Decode(&got); err != nil {
		t.Fatalf("decode failed: %v", err)
	}
	if len(got) != 0 {
		t.Fatalf("projects = %+v, want empty", got)
	}
}
