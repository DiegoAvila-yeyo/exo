package termserver

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/yeyoos/nucleo-base/shared/api"
)

func TestChatPostReturns409WhenTurnBusy(t *testing.T) {
	store := newFakeStore()
	blocked := make(chan struct{})
	release := make(chan struct{})

	_, server, httpServer := newTestServer(t, store, WithAgentRunner(func(_ context.Context, _ string, _ []api.Message, _ string, _ string, _ string, _ string) ([]api.Message, *NavigateAction, *CanvasSuggestion, error) {
		close(blocked)
		<-release
		return nil, nil, nil, nil
	}))
	client, csrf := bootstrapClient(t, httpServer, server)

	firstReq := newJSONRequest(t, http.MethodPost, httpServer.URL+"/api/chat?csrf_token="+url.QueryEscape(csrf), `{"message":"one"}`)
	firstReq.Header.Set("Origin", allowedOrigin(server))
	firstReq.Header.Set("Content-Type", "application/json")
	firstResp, err := client.Do(firstReq)
	if err != nil {
		t.Fatalf("first POST /api/chat failed: %v", err)
	}
	firstResp.Body.Close()
	if firstResp.StatusCode != http.StatusAccepted {
		t.Fatalf("first status = %d, want %d", firstResp.StatusCode, http.StatusAccepted)
	}

	select {
	case <-blocked:
	case <-time.After(2 * time.Second):
		t.Fatal("runner never blocked")
	}

	secondReq := newJSONRequest(t, http.MethodPost, httpServer.URL+"/api/chat?csrf_token="+url.QueryEscape(csrf), `{"message":"two"}`)
	secondReq.Header.Set("Origin", allowedOrigin(server))
	secondReq.Header.Set("Content-Type", "application/json")
	secondResp, err := client.Do(secondReq)
	if err != nil {
		t.Fatalf("second POST /api/chat failed: %v", err)
	}
	defer secondResp.Body.Close()
	if secondResp.StatusCode != http.StatusConflict {
		t.Fatalf("second status = %d, want %d", secondResp.StatusCode, http.StatusConflict)
	}
	assertJSONBody(t, secondResp.Body, map[string]any{"error": "busy"})
	close(release)
}

func TestChatStreamEmitsIdleBusyOutputApprovalDoneWithExactSchema(t *testing.T) {
	store := newFakeStore()
	var serverRef *Server
	approvalRequested := make(chan struct{})

	runner := func(_ context.Context, _ string, _ []api.Message, _ string, _ string, _ string, _ string) ([]api.Message, *NavigateAction, *CanvasSuggestion, error) {
		_, _ = io.WriteString(serverRef.ChatOutputWriter(), "hello from runner\n")
		close(approvalRequested)
		if !serverRef.RequestApproval("Approve shell write?", "detail text", map[string]string{
			"tool":       "terminal_write",
			"session_id": "session-0007",
			"command":    "python app.py",
		}) {
			t.Errorf("approval callback returned false, want true")
		}
		return nil, nil, nil, nil
	}

	_, server, httpServer := newTestServer(t, store, WithAgentRunner(runner))
	serverRef = server
	client, csrf := bootstrapClient(t, httpServer, server)

	idleResp := openChatStream(t, client, httpServer, server, true)
	defer idleResp.Body.Close()
	idleReader := bufio.NewReader(idleResp.Body)
	assertSSEEvent(t, readSSEEvent(t, idleReader), map[string]any{"type": "idle"})

	startChat(t, client, httpServer, server, csrf, "hello")

	busyResp := openChatStream(t, client, httpServer, server, false)
	defer busyResp.Body.Close()
	busyReader := bufio.NewReader(busyResp.Body)
	assertSSEEvent(t, readSSEEvent(t, busyReader), map[string]any{"type": "busy"})

	select {
	case <-approvalRequested:
	case <-time.After(2 * time.Second):
		t.Fatal("approval was never requested")
	}

	seenOutput := false
	seenApproval := false
	for !(seenOutput && seenApproval) {
		event := readSSEEvent(t, busyReader)
		var decoded map[string]any
		if err := json.Unmarshal(event, &decoded); err != nil {
			t.Fatalf("unmarshal SSE payload %q failed: %v", string(event), err)
		}
		switch decoded["type"] {
		case "output":
			assertSSEEvent(t, event, map[string]any{
				"type": "output",
				"text": "hello from runner\n",
			})
			seenOutput = true
		case "approval":
			assertSSEEvent(t, event, map[string]any{
				"type":       "approval",
				"prompt":     "Approve shell write?",
				"detail":     "detail text",
				"session_id": "session-0007",
			})
			seenApproval = true
		default:
			t.Fatalf("unexpected SSE payload before output+approval: %#v", decoded)
		}
	}

	approveResp := postApprove(t, client, httpServer, server, csrf, true)
	approveResp.Body.Close()
	assertSSEEvent(t, readSSEEvent(t, busyReader), map[string]any{"type": "done"})
}

func TestChatStreamStripsANSIFromOutputEvents(t *testing.T) {
	store := newFakeStore()
	var serverRef *Server
	blocked := make(chan struct{})
	release := make(chan struct{})

	runner := func(_ context.Context, _ string, _ []api.Message, _ string, _ string, _ string, _ string) ([]api.Message, *NavigateAction, *CanvasSuggestion, error) {
		_, _ = io.WriteString(serverRef.ChatOutputWriter(), "\x1b[36mhello\x1b[0m world\n")
		close(blocked)
		<-release
		return nil, nil, nil, nil
	}

	_, server, httpServer := newTestServer(t, store, WithAgentRunner(runner))
	serverRef = server
	client, csrf := bootstrapClient(t, httpServer, server)

	startChat(t, client, httpServer, server, csrf, "ansi")
	select {
	case <-blocked:
	case <-time.After(2 * time.Second):
		t.Fatal("runner never blocked")
	}

	resp := openChatStream(t, client, httpServer, server, false)
	defer resp.Body.Close()
	reader := bufio.NewReader(resp.Body)

	assertSSEEvent(t, readSSEEvent(t, reader), map[string]any{"type": "busy"})
	assertSSEEvent(t, readSSEEvent(t, reader), map[string]any{
		"type": "output",
		"text": "hello world\n",
	})
	close(release)
	assertSSEEvent(t, readSSEEvent(t, reader), map[string]any{"type": "done"})
}

func TestApprovalRoutesResolvePendingApproval(t *testing.T) {
	store := newFakeStore()
	_, server, httpServer := newTestServer(t, store)
	client, csrf := bootstrapClient(t, httpServer, server)

	decisionCh := make(chan bool, 1)
	go func() {
		decisionCh <- server.RequestApproval("prompt", "detail", map[string]string{"session_id": "session-a"})
	}()

	resp := postApprove(t, client, httpServer, server, csrf, true)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("approve(true) status = %d, want %d", resp.StatusCode, http.StatusOK)
	}
	assertJSONBody(t, resp.Body, map[string]any{"status": "ok"})
	resp.Body.Close()
	select {
	case approved := <-decisionCh:
		if !approved {
			t.Fatal("approved = false, want true")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("approval(true) was not delivered")
	}

	go func() {
		decisionCh <- server.RequestApproval("prompt-2", "detail-2", map[string]string{"session_id": "session-b"})
	}()
	resp = postApprove(t, client, httpServer, server, csrf, false)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("approve(false) status = %d, want %d", resp.StatusCode, http.StatusOK)
	}
	assertJSONBody(t, resp.Body, map[string]any{"status": "ok"})
	resp.Body.Close()
	select {
	case approved := <-decisionCh:
		if approved {
			t.Fatal("approved = true, want false")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("approval(false) was not delivered")
	}

	resp = postApprove(t, client, httpServer, server, csrf, true)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusConflict {
		t.Fatalf("approve(no pending) status = %d, want %d", resp.StatusCode, http.StatusConflict)
	}
	assertJSONBody(t, resp.Body, map[string]any{"error": "no pending approval"})
}

func TestChatRoutesEnforceOriginAndCSRF(t *testing.T) {
	store := newFakeStore()
	_, server, httpServer := newTestServer(t, store, WithAgentRunner(func(_ context.Context, _ string, _ []api.Message, _ string, _ string, _ string, _ string) ([]api.Message, *NavigateAction, *CanvasSuggestion, error) {
		return nil, nil, nil, nil
	}))
	client, csrf := bootstrapClient(t, httpServer, server)

	noOriginReq := newJSONRequest(t, http.MethodPost, httpServer.URL+"/api/chat?csrf_token="+url.QueryEscape(csrf), `{"message":"hello"}`)
	noOriginReq.Header.Set("Content-Type", "application/json")
	noOriginResp, err := client.Do(noOriginReq)
	if err != nil {
		t.Fatalf("POST /api/chat without origin failed: %v", err)
	}
	defer noOriginResp.Body.Close()
	if noOriginResp.StatusCode != http.StatusForbidden {
		t.Fatalf("chat without origin status = %d, want %d", noOriginResp.StatusCode, http.StatusForbidden)
	}

	badOriginReq := newJSONRequest(t, http.MethodPost, httpServer.URL+"/api/approve?csrf_token="+url.QueryEscape(csrf), `{"approved":true}`)
	badOriginReq.Header.Set("Origin", "http://evil.example:9999")
	badOriginReq.Header.Set("Content-Type", "application/json")
	badOriginResp, err := client.Do(badOriginReq)
	if err != nil {
		t.Fatalf("POST /api/approve with bad origin failed: %v", err)
	}
	defer badOriginResp.Body.Close()
	if badOriginResp.StatusCode != http.StatusForbidden {
		t.Fatalf("approve bad origin status = %d, want %d", badOriginResp.StatusCode, http.StatusForbidden)
	}

	missingCSRFReq := newJSONRequest(t, http.MethodPost, httpServer.URL+"/api/approve", `{"approved":true}`)
	missingCSRFReq.Header.Set("Origin", allowedOrigin(server))
	missingCSRFReq.Header.Set("Content-Type", "application/json")
	missingCSRFResp, err := client.Do(missingCSRFReq)
	if err != nil {
		t.Fatalf("POST /api/approve without csrf failed: %v", err)
	}
	defer missingCSRFResp.Body.Close()
	if missingCSRFResp.StatusCode != http.StatusForbidden {
		t.Fatalf("approve missing csrf status = %d, want %d", missingCSRFResp.StatusCode, http.StatusForbidden)
	}

	streamReq, err := http.NewRequest(http.MethodGet, httpServer.URL+"/api/chat/stream", nil)
	if err != nil {
		t.Fatalf("build stream request failed: %v", err)
	}
	streamResp, err := client.Do(streamReq)
	if err != nil {
		t.Fatalf("GET /api/chat/stream failed: %v", err)
	}
	defer streamResp.Body.Close()
	if streamResp.StatusCode != http.StatusOK {
		t.Fatalf("stream status = %d, want %d", streamResp.StatusCode, http.StatusOK)
	}
}

func openChatStream(t *testing.T, client *http.Client, httpServer *httptest.Server, server *Server, withOrigin bool) *http.Response {
	t.Helper()
	req, err := http.NewRequest(http.MethodGet, httpServer.URL+"/api/chat/stream", nil)
	if err != nil {
		t.Fatalf("build chat stream request failed: %v", err)
	}
	if withOrigin {
		req.Header.Set("Origin", allowedOrigin(server))
	}
	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("GET /api/chat/stream failed: %v", err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("GET /api/chat/stream status = %d, want %d", resp.StatusCode, http.StatusOK)
	}
	return resp
}

func startChat(t *testing.T, client *http.Client, httpServer *httptest.Server, server *Server, csrf, message string) {
	t.Helper()
	req := newJSONRequest(t, http.MethodPost, httpServer.URL+"/api/chat?csrf_token="+url.QueryEscape(csrf), `{"message":"`+message+`"}`)
	req.Header.Set("Origin", allowedOrigin(server))
	req.Header.Set("Content-Type", "application/json")
	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("POST /api/chat failed: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusAccepted {
		t.Fatalf("POST /api/chat status = %d, want %d", resp.StatusCode, http.StatusAccepted)
	}
	assertJSONBody(t, resp.Body, map[string]any{"status": "accepted"})
}

func postApprove(t *testing.T, client *http.Client, httpServer *httptest.Server, server *Server, csrf string, approved bool) *http.Response {
	t.Helper()
	req := newJSONRequest(t, http.MethodPost, httpServer.URL+"/api/approve?csrf_token="+url.QueryEscape(csrf), string(mustJSON(t, map[string]bool{"approved": approved})))
	req.Header.Set("Origin", allowedOrigin(server))
	req.Header.Set("Content-Type", "application/json")
	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("POST /api/approve failed: %v", err)
	}
	return resp
}

func readSSEEvent(t *testing.T, reader *bufio.Reader) []byte {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		line, err := reader.ReadString('\n')
		if err != nil {
			t.Fatalf("read SSE line failed: %v", err)
		}
		if strings.HasPrefix(line, "data: ") {
			return bytes.TrimSpace([]byte(strings.TrimPrefix(line, "data: ")))
		}
	}
	t.Fatal("timed out waiting for SSE event")
	return nil
}

func assertSSEEvent(t *testing.T, raw []byte, want map[string]any) {
	t.Helper()
	var got map[string]any
	if err := json.Unmarshal(raw, &got); err != nil {
		t.Fatalf("unmarshal SSE payload %q failed: %v", string(raw), err)
	}
	if len(got) != len(want) {
		t.Fatalf("SSE payload = %#v, want exact schema %#v", got, want)
	}
	for key, wantValue := range want {
		if got[key] != wantValue {
			t.Fatalf("SSE payload[%s] = %#v, want %#v (full payload %#v)", key, got[key], wantValue, got)
		}
	}
}

func assertJSONBody(t *testing.T, body io.Reader, want map[string]any) {
	t.Helper()
	var got map[string]any
	if err := json.NewDecoder(body).Decode(&got); err != nil {
		t.Fatalf("decode JSON body failed: %v", err)
	}
	if len(got) != len(want) {
		t.Fatalf("JSON body = %#v, want exact schema %#v", got, want)
	}
	for key, wantValue := range want {
		if got[key] != wantValue {
			t.Fatalf("JSON body[%s] = %#v, want %#v (full payload %#v)", key, got[key], wantValue, got)
		}
	}
}

func newJSONRequest(t *testing.T, method, rawURL, body string) *http.Request {
	t.Helper()
	req, err := http.NewRequest(method, rawURL, strings.NewReader(body))
	if err != nil {
		t.Fatalf("build request failed: %v", err)
	}
	return req
}

func mustJSON(t *testing.T, value any) []byte {
	t.Helper()
	data, err := json.Marshal(value)
	if err != nil {
		t.Fatalf("marshal JSON failed: %v", err)
	}
	return data
}
