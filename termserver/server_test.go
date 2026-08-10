package termserver

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"net/url"
	"regexp"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/DiegoAvila-yeyo/exo/ptyactor"
	"github.com/DiegoAvila-yeyo/exo/sessions"
	"github.com/gorilla/websocket"
)

func TestWebSocketUpgradeSucceedsWithValidOriginAndToken(t *testing.T) {
	store := newFakeStore()
	store.addSession("alpha", newFakePTY())
	_, server, httpServer := newTestServer(t, store)

	conn := dialWS(t, httpServer, server.Token(), allowedOrigin(server), "/api/terminal/alpha/stream")
	defer conn.Close()

	ready := readReady(t, conn)
	if ready.Type != "ready" {
		t.Fatalf("ready type = %q, want %q", ready.Type, "ready")
	}
	if conn.Subprotocol() != tokenSubprotocol+server.Token() {
		t.Fatalf("subprotocol = %q, want echoed token subprotocol", conn.Subprotocol())
	}
}

func TestBinaryTerminalInputReachesPTYAfterTakeover(t *testing.T) {
	store := newFakeStore()
	pty := newFakePTY()
	store.addSession("alpha", pty)
	_, server, httpServer := newTestServer(t, store)

	conn := dialWS(t, httpServer, server.Token(), allowedOrigin(server), "/api/terminal/alpha/stream")
	defer conn.Close()

	ready := readReady(t, conn)
	if ready.Type != "ready" {
		t.Fatalf("ready type = %q, want %q", ready.Type, "ready")
	}

	if err := conn.WriteJSON(controlMessage{Type: "takeover", Owner: "human"}); err != nil {
		t.Fatalf("takeover failed: %v", err)
	}
	lease := readStatusWithDeadline(t, conn)
	if lease.Type != "lease" || lease.Owner != "human" {
		t.Fatalf("lease = %+v, want human lease", lease)
	}

	payload := []byte("echo browser-contract\n")
	if err := conn.WriteMessage(websocket.BinaryMessage, payload); err != nil {
		t.Fatalf("binary write failed: %v", err)
	}

	select {
	case written := <-pty.writeSeen:
		if string(written) != string(payload) {
			t.Fatalf("pty write = %q, want %q", written, payload)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for PTY write")
	}
}

func TestIndexServesEmbeddedFrontendAssets(t *testing.T) {
	store := newFakeStore()
	_, server, httpServer := newTestServer(t, store)

	req, err := http.NewRequest(http.MethodGet, httpServer.URL+"/", nil)
	if err != nil {
		t.Fatalf("build index request failed: %v", err)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("index request failed: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("index status = %d, want %d", resp.StatusCode, http.StatusOK)
	}
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read index body failed: %v", err)
	}
	htmlBody := string(body)
	if !strings.Contains(htmlBody, `data-app="exo-terminal"`) {
		t.Fatalf("index missing frontend app marker: %q", htmlBody)
	}
	if metaContent(t, htmlBody, tokenMetaName) != server.Token() {
		t.Fatalf("index token meta mismatch")
	}
	if metaContent(t, htmlBody, csrfMetaName) == "" {
		t.Fatal("index csrf meta missing")
	}

	for _, path := range []string{
		"/assets/app.css",
		"/assets/app.js",
		"/assets/vendor/xterm/xterm.css",
		"/assets/vendor/xterm/xterm.js",
		"/assets/vendor/xterm-addon-fit/addon-fit.js",
	} {
		resp, err := http.Get(httpServer.URL + path)
		if err != nil {
			t.Fatalf("GET %s failed: %v", path, err)
		}
		resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("GET %s status = %d, want %d", path, resp.StatusCode, http.StatusOK)
		}
	}
}

func TestGetSessionsAllowsMissingOriginHeader(t *testing.T) {
	store := newFakeStore()
	store.addSession("alpha", newFakePTY())
	_, _, httpServer := newTestServer(t, store)

	req, err := http.NewRequest(http.MethodGet, httpServer.URL+"/api/sessions", nil)
	if err != nil {
		t.Fatalf("build request failed: %v", err)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("GET /api/sessions failed: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want %d", resp.StatusCode, http.StatusOK)
	}

	var listed []sessions.SessionInfo
	if err := json.NewDecoder(resp.Body).Decode(&listed); err != nil {
		t.Fatalf("decode response failed: %v", err)
	}
	if len(listed) != 1 || listed[0].ID != "alpha" {
		t.Fatalf("listed sessions = %+v, want session alpha", listed)
	}
}

func TestEmbeddedAssetContentTypes(t *testing.T) {
	store := newFakeStore()
	_, _, httpServer := newTestServer(t, store)

	cases := []struct {
		path string
		want string
	}{
		{path: "/assets/app.css", want: "text/css"},
		{path: "/assets/app.js", want: "javascript"},
		{path: "/assets/vendor/xterm/xterm.css", want: "text/css"},
		{path: "/assets/vendor/xterm/xterm.js", want: "javascript"},
		{path: "/assets/vendor/xterm-addon-fit/addon-fit.js", want: "javascript"},
	}

	for _, tc := range cases {
		resp, err := http.Get(httpServer.URL + tc.path)
		if err != nil {
			t.Fatalf("GET %s failed: %v", tc.path, err)
		}
		resp.Body.Close()
		contentType := resp.Header.Get("Content-Type")
		if !strings.Contains(contentType, tc.want) {
			t.Fatalf("GET %s content-type = %q, want substring %q", tc.path, contentType, tc.want)
		}
	}
}

func TestServedStylesheetIncludesTerminalOverlayHiddenOverride(t *testing.T) {
	store := newFakeStore()
	_, _, httpServer := newTestServer(t, store)

	resp, err := http.Get(httpServer.URL + "/assets/app.css")
	if err != nil {
		t.Fatalf("GET /assets/app.css failed: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want %d", resp.StatusCode, http.StatusOK)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read stylesheet failed: %v", err)
	}
	css := string(body)
	if !strings.Contains(css, ".terminal-overlay[hidden]") {
		t.Fatal("stylesheet missing .terminal-overlay[hidden] override")
	}
	if !strings.Contains(css, "display: none;") {
		t.Fatal("stylesheet missing display:none hidden override")
	}
}

func TestServedAppJSUsesBinaryFramesForTerminalInput(t *testing.T) {
	store := newFakeStore()
	_, _, httpServer := newTestServer(t, store)

	resp, err := http.Get(httpServer.URL + "/assets/app.js")
	if err != nil {
		t.Fatalf("GET /assets/app.js failed: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want %d", resp.StatusCode, http.StatusOK)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read app.js failed: %v", err)
	}
	script := string(body)
	if !strings.Contains(script, "new TextEncoder()") {
		t.Fatal("app.js missing TextEncoder initialization for terminal input")
	}
	if !strings.Contains(script, "state.socket.send(inputEncoder.encode(data));") {
		t.Fatal("app.js missing binary terminal input send path")
	}
	if !strings.Contains(script, `state.socket.send(JSON.stringify({type: "takeover", owner: "human"}));`) {
		t.Fatal("app.js missing text takeover control send path")
	}
	if !strings.Contains(script, `state.socket.send(JSON.stringify({type: "resize", cols: terminal.cols, rows: terminal.rows}));`) {
		t.Fatal("app.js missing text resize control send path")
	}
}

func TestWebSocketUpgradeRejectsInvalidOrigin(t *testing.T) {
	store := newFakeStore()
	store.addSession("alpha", newFakePTY())
	_, server, httpServer := newTestServer(t, store)

	dialer := websocket.Dialer{Subprotocols: []string{tokenSubprotocol + server.Token()}}
	_, resp, err := dialer.Dial(wsURL(httpServer.URL, "alpha"), http.Header{
		"Origin": []string{"http://evil.example:9999"},
	})
	if err == nil {
		t.Fatal("expected handshake failure for invalid origin")
	}
	if resp == nil || resp.StatusCode != http.StatusForbidden {
		t.Fatalf("status = %v, want %d", statusCode(resp), http.StatusForbidden)
	}
}

func TestWebSocketUpgradeRejectsWrongToken(t *testing.T) {
	store := newFakeStore()
	store.addSession("alpha", newFakePTY())
	_, server, httpServer := newTestServer(t, store)

	dialer := websocket.Dialer{Subprotocols: []string{tokenSubprotocol + "wrong-token"}}
	_, resp, err := dialer.Dial(wsURL(httpServer.URL, "alpha"), http.Header{
		"Origin": []string{allowedOrigin(server)},
	})
	if err == nil {
		t.Fatal("expected handshake failure for wrong token")
	}
	if resp == nil || resp.StatusCode != http.StatusForbidden {
		t.Fatalf("status = %v, want %d", statusCode(resp), http.StatusForbidden)
	}
}

func TestTakeoverKeepsOtherClientsStreamingOutput(t *testing.T) {
	store := newFakeStore()
	pty := newFakePTY()
	store.addSession("alpha", pty)
	_, server, httpServer := newTestServer(t, store)

	first := dialWS(t, httpServer, server.Token(), allowedOrigin(server), "/api/terminal/alpha/stream")
	defer first.Close()
	second := dialWS(t, httpServer, server.Token(), allowedOrigin(server), "/api/terminal/alpha/stream")
	defer second.Close()

	readReady(t, first)
	readReady(t, second)

	if err := first.WriteJSON(controlMessage{Type: "takeover", Owner: "human"}); err != nil {
		t.Fatalf("write takeover failed: %v", err)
	}

	firstLease := readStatusWithDeadline(t, first)
	secondLease := readStatusWithDeadline(t, second)
	if firstLease.Type != "lease" || secondLease.Type != "lease" {
		t.Fatalf("lease broadcasts = %+v %+v, want lease/lease", firstLease, secondLease)
	}

	pty.EmitOutput([]byte("after-takeover"))
	messageType, payload, err := readMessageWithDeadline(t, second)
	if err != nil {
		t.Fatalf("read output failed: %v", err)
	}
	if messageType != websocket.BinaryMessage || string(payload) != "after-takeover" {
		t.Fatalf("message = (%d,%q), want binary/after-takeover", messageType, payload)
	}
}

func TestBroadcastLeaseDoesNotBlockOnFullClientChannel(t *testing.T) {
	server := &Server{
		hubs: map[string]*sessionHub{
			"alpha": {
				clients: make(map[*wsClient]struct{}),
			},
		},
	}
	slow := &wsClient{leaseUpdates: make(chan leaseUpdate, 1)}
	fast := &wsClient{leaseUpdates: make(chan leaseUpdate, 1)}
	server.hubs["alpha"].clients[slow] = struct{}{}
	server.hubs["alpha"].clients[fast] = struct{}{}
	slow.leaseUpdates <- leaseUpdate{}

	done := make(chan struct{})
	go func() {
		server.broadcastLease("alpha", ptyactor.Lease{Owner: "human", Epoch: 2}, fast)
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("broadcastLease blocked on full client channel")
	}

	select {
	case update := <-fast.leaseUpdates:
		if update.lease.Owner != "human" || update.lease.Epoch != 2 || !update.canWrite {
			t.Fatalf("fast client update = %+v, want human epoch 2 writable lease", update)
		}
	default:
		t.Fatal("fast client did not receive lease update")
	}
}

func TestGetAndPostSessionsEndToEnd(t *testing.T) {
	manager := sessions.New()
	_, server, httpServer := newTestServer(t, manager)

	client, csrf := bootstrapClient(t, httpServer, server)
	workdir := t.TempDir()
	createURL := httpServer.URL + "/api/sessions?csrf_token=" + url.QueryEscape(csrf)
	createReq, err := http.NewRequest(http.MethodPost, createURL, strings.NewReader(`{"workdir":"`+workdir+`","name":"demo"}`))
	if err != nil {
		t.Fatalf("build create request failed: %v", err)
	}
	createReq.Header.Set("Origin", allowedOrigin(server))
	createReq.Header.Set("Content-Type", "application/json")
	createResp, err := client.Do(createReq)
	if err != nil {
		t.Fatalf("create request failed: %v", err)
	}
	defer createResp.Body.Close()
	if createResp.StatusCode != http.StatusCreated {
		t.Fatalf("create status = %d, want %d", createResp.StatusCode, http.StatusCreated)
	}
	var created sessions.SessionInfo
	if err := json.NewDecoder(createResp.Body).Decode(&created); err != nil {
		t.Fatalf("decode created session failed: %v", err)
	}
	t.Cleanup(func() {
		_ = manager.Close(created.ID)
	})

	listReq, err := http.NewRequest(http.MethodGet, httpServer.URL+"/api/sessions", nil)
	if err != nil {
		t.Fatalf("build list request failed: %v", err)
	}
	listReq.Header.Set("Origin", allowedOrigin(server))
	listResp, err := client.Do(listReq)
	if err != nil {
		t.Fatalf("list request failed: %v", err)
	}
	defer listResp.Body.Close()
	if listResp.StatusCode != http.StatusOK {
		t.Fatalf("list status = %d, want %d", listResp.StatusCode, http.StatusOK)
	}
	var listed []sessions.SessionInfo
	if err := json.NewDecoder(listResp.Body).Decode(&listed); err != nil {
		t.Fatalf("decode list failed: %v", err)
	}
	if len(listed) != 1 || listed[0].ID != created.ID {
		t.Fatalf("listed sessions = %+v, want one session %q", listed, created.ID)
	}

	conn := dialWS(t, httpServer, server.Token(), allowedOrigin(server), "/api/terminal/"+created.ID+"/stream")
	defer conn.Close()
	readReady(t, conn)

	if err := conn.WriteMessage(websocket.BinaryMessage, []byte("echo hello-from-server\n")); err != nil {
		t.Fatalf("write command failed: %v", err)
	}
	output := readBinaryUntilContains(t, conn, "hello-from-server")
	if !strings.Contains(output, "hello-from-server") {
		t.Fatalf("output = %q, want substring %q", output, "hello-from-server")
	}
}

func TestWebSocketMissingSessionReturns404(t *testing.T) {
	manager := sessions.New()
	_, server, httpServer := newTestServer(t, manager)

	dialer := websocket.Dialer{Subprotocols: []string{tokenSubprotocol + server.Token()}}
	_, resp, err := dialer.Dial(wsURL(httpServer.URL, "missing"), http.Header{
		"Origin": []string{allowedOrigin(server)},
	})
	if err == nil {
		t.Fatal("expected handshake failure for missing session")
	}
	if resp == nil || resp.StatusCode != http.StatusNotFound {
		t.Fatalf("status = %v, want %d", statusCode(resp), http.StatusNotFound)
	}
}

func TestTakeoverOnSessionADoesNotAffectSessionB(t *testing.T) {
	manager := sessions.New()
	_, server, httpServer := newTestServer(t, manager)

	client, csrf := bootstrapClient(t, httpServer, server)
	workdirA := t.TempDir()
	workdirB := t.TempDir()
	sessionA := createSessionViaHTTP(t, client, httpServer, server, csrf, workdirA, "alpha")
	sessionB := createSessionViaHTTP(t, client, httpServer, server, csrf, workdirB, "beta")
	t.Cleanup(func() {
		_ = manager.Close(sessionA.ID)
		_ = manager.Close(sessionB.ID)
	})

	connA := dialWS(t, httpServer, server.Token(), allowedOrigin(server), "/api/terminal/"+sessionA.ID+"/stream")
	defer connA.Close()
	connB := dialWS(t, httpServer, server.Token(), allowedOrigin(server), "/api/terminal/"+sessionB.ID+"/stream")
	defer connB.Close()

	readyA := readReady(t, connA)
	readyB := readReady(t, connB)
	if readyA.Epoch != 1 || readyB.Epoch != 1 {
		t.Fatalf("initial epochs = %d and %d, want 1 and 1", readyA.Epoch, readyB.Epoch)
	}

	if err := connA.WriteJSON(controlMessage{Type: "takeover", Owner: "human"}); err != nil {
		t.Fatalf("takeover on session A failed: %v", err)
	}
	leaseA := readStatusWithDeadline(t, connA)
	if leaseA.Type != "lease" || leaseA.Epoch != 2 {
		t.Fatalf("leaseA = %+v, want epoch 2 lease", leaseA)
	}

	if err := connB.WriteMessage(websocket.BinaryMessage, []byte("echo independent-session-b\n")); err != nil {
		t.Fatalf("write to session B failed: %v", err)
	}
	outputB := readBinaryUntilContains(t, connB, "independent-session-b")
	if !strings.Contains(outputB, "independent-session-b") {
		t.Fatalf("outputB = %q, want substring %q", outputB, "independent-session-b")
	}
}

func newTestServer(t *testing.T, store sessionStore, opts ...Option) (sessionStore, *Server, *httptest.Server) {
	t.Helper()

	if fake, ok := store.(*fakeStore); ok {
		t.Cleanup(func() {
			fake.closeAll()
		})
	}

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen failed: %v", err)
	}
	port := listener.Addr().(*net.TCPAddr).Port

	server, err := New(port, store, opts...)
	if err != nil {
		t.Fatalf("new server failed: %v", err)
	}

	httpServer := httptest.NewUnstartedServer(server)
	httpServer.Listener = listener
	httpServer.Start()
	t.Cleanup(httpServer.Close)

	return store, server, httpServer
}

func bootstrapClient(t *testing.T, httpServer *httptest.Server, server *Server) (*http.Client, string) {
	t.Helper()
	jar, err := cookiejar.New(nil)
	if err != nil {
		t.Fatalf("cookie jar failed: %v", err)
	}
	client := &http.Client{Jar: jar}
	req, err := http.NewRequest(http.MethodGet, httpServer.URL+"/", nil)
	if err != nil {
		t.Fatalf("bootstrap request failed: %v", err)
	}
	getResp, err := client.Do(req)
	if err != nil {
		t.Fatalf("bootstrap GET failed: %v", err)
	}
	body, err := io.ReadAll(getResp.Body)
	getResp.Body.Close()
	if err != nil {
		t.Fatalf("read bootstrap failed: %v", err)
	}
	return client, metaContent(t, string(body), csrfMetaName)
}

func createSessionViaHTTP(
	t *testing.T,
	client *http.Client,
	httpServer *httptest.Server,
	server *Server,
	csrf, workdir, name string,
) sessions.SessionInfo {
	t.Helper()
	createURL := httpServer.URL + "/api/sessions?csrf_token=" + url.QueryEscape(csrf)
	body := fmt.Sprintf(`{"workdir":%q,"name":%q}`, workdir, name)
	req, err := http.NewRequest(http.MethodPost, createURL, strings.NewReader(body))
	if err != nil {
		t.Fatalf("build create request failed: %v", err)
	}
	req.Header.Set("Origin", allowedOrigin(server))
	req.Header.Set("Content-Type", "application/json")
	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("create request failed: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("create status = %d, want %d", resp.StatusCode, http.StatusCreated)
	}
	var info sessions.SessionInfo
	if err := json.NewDecoder(resp.Body).Decode(&info); err != nil {
		t.Fatalf("decode create response failed: %v", err)
	}
	return info
}

func dialWS(t *testing.T, httpServer *httptest.Server, token, origin, path string) *websocket.Conn {
	t.Helper()
	dialer := websocket.Dialer{Subprotocols: []string{tokenSubprotocol + token}}
	conn, resp, err := dialer.Dial("ws"+strings.TrimPrefix(httpServer.URL, "http")+path, http.Header{
		"Origin": []string{origin},
	})
	if err != nil {
		t.Fatalf("dial failed: %v (status=%v)", err, statusCode(resp))
	}
	return conn
}

func allowedOrigin(server *Server) string {
	return "http://127.0.0.1:" + server.port
}

func wsURL(raw, sessionID string) string {
	return "ws" + strings.TrimPrefix(raw, "http") + "/api/terminal/" + sessionID + "/stream"
}

func readReady(t *testing.T, conn *websocket.Conn) statusMessage {
	t.Helper()
	var ready statusMessage
	if err := conn.ReadJSON(&ready); err != nil {
		t.Fatalf("read ready failed: %v", err)
	}
	return ready
}

func readStatusWithDeadline(t *testing.T, conn *websocket.Conn) statusMessage {
	t.Helper()
	if err := conn.SetReadDeadline(time.Now().Add(2 * time.Second)); err != nil {
		t.Fatalf("set read deadline failed: %v", err)
	}
	defer func() { _ = conn.SetReadDeadline(time.Time{}) }()
	var msg statusMessage
	if err := conn.ReadJSON(&msg); err != nil {
		t.Fatalf("read status failed: %v", err)
	}
	return msg
}

func readMessageWithDeadline(t *testing.T, conn *websocket.Conn) (int, []byte, error) {
	t.Helper()
	if err := conn.SetReadDeadline(time.Now().Add(3 * time.Second)); err != nil {
		t.Fatalf("set read deadline failed: %v", err)
	}
	defer func() { _ = conn.SetReadDeadline(time.Time{}) }()
	return conn.ReadMessage()
}

func readBinaryUntilContains(t *testing.T, conn *websocket.Conn, want string) string {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	var builder strings.Builder
	for time.Now().Before(deadline) {
		messageType, payload, err := readMessageWithDeadline(t, conn)
		if err != nil {
			t.Fatalf("read binary failed: %v", err)
		}
		if messageType != websocket.BinaryMessage {
			continue
		}
		builder.Write(payload)
		if strings.Contains(builder.String(), want) {
			return builder.String()
		}
	}
	t.Fatalf("timed out waiting for %q in %q", want, builder.String())
	return ""
}

func metaContent(t *testing.T, htmlBody, name string) string {
	t.Helper()
	pattern := regexp.MustCompile(`<meta name="` + regexp.QuoteMeta(name) + `" content="([^"]+)">`)
	matches := pattern.FindStringSubmatch(htmlBody)
	if len(matches) != 2 {
		t.Fatalf("meta %q not found", name)
	}
	return matches[1]
}

func statusCode(resp *http.Response) int {
	if resp == nil {
		return 0
	}
	return resp.StatusCode
}

type fakeStore struct {
	mu       sync.RWMutex
	sessions map[string]*fakeSession
}

type fakeSession struct {
	session *ptyactor.Session
	info    sessions.SessionInfo
}

func newFakeStore() *fakeStore {
	return &fakeStore{sessions: make(map[string]*fakeSession)}
}

func (s *fakeStore) addSession(id string, pty *fakePTY) {
	s.mu.Lock()
	defer s.mu.Unlock()
	now := time.Now()
	s.sessions[id] = &fakeSession{
		session: ptyactor.NewSession(pty),
		info: sessions.SessionInfo{
			ID:           id,
			Name:         id,
			Workdir:      "/tmp",
			Command:      "fake-shell",
			Status:       "running",
			CreatedAt:    now,
			LastActiveAt: now,
		},
	}
}

func (s *fakeStore) Create(workdir, name string) (sessions.SessionInfo, error) {
	return sessions.SessionInfo{}, errors.New("not implemented for fake store")
}

func (s *fakeStore) List() []sessions.SessionInfo {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]sessions.SessionInfo, 0, len(s.sessions))
	for _, session := range s.sessions {
		out = append(out, session.info)
	}
	return out
}

func (s *fakeStore) Get(id string) (*ptyactor.Session, sessions.SessionInfo, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	session, ok := s.sessions[id]
	if !ok {
		return nil, sessions.SessionInfo{}, false
	}
	return session.session, session.info, true
}

func (s *fakeStore) Close(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	session, ok := s.sessions[id]
	if !ok {
		return io.EOF
	}
	delete(s.sessions, id)
	return session.session.Close()
}

func (s *fakeStore) Touch(id string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	session, ok := s.sessions[id]
	if !ok {
		return false
	}
	session.info.LastActiveAt = time.Now()
	return true
}

func (s *fakeStore) closeAll() {
	s.mu.Lock()
	defer s.mu.Unlock()
	for id, session := range s.sessions {
		_ = session.session.Close()
		delete(s.sessions, id)
	}
}

type fakePTY struct {
	mu         sync.Mutex
	writes     bytes.Buffer
	readCh     chan []byte
	closeCh    chan struct{}
	writeSeen  chan []byte
	resizeSeen chan resizeCall
	closed     bool
}

type resizeCall struct {
	cols int
	rows int
}

func newFakePTY() *fakePTY {
	return &fakePTY{
		readCh:     make(chan []byte),
		closeCh:    make(chan struct{}),
		writeSeen:  make(chan []byte, 8),
		resizeSeen: make(chan resizeCall, 8),
	}
}

func (f *fakePTY) Write(p []byte) (int, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.closed {
		return 0, io.EOF
	}
	n, err := f.writes.Write(p)
	if err == nil {
		f.writeSeen <- append([]byte(nil), p...)
	}
	return n, err
}

func (f *fakePTY) Read(p []byte) (int, error) {
	select {
	case <-f.closeCh:
		return 0, io.EOF
	case chunk := <-f.readCh:
		return copy(p, chunk), nil
	}
}

func (f *fakePTY) Resize(cols, rows int) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.closed {
		return io.EOF
	}
	f.resizeSeen <- resizeCall{cols: cols, rows: rows}
	return nil
}

func (f *fakePTY) Close() error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.closed {
		return nil
	}
	f.closed = true
	close(f.closeCh)
	return nil
}

func (f *fakePTY) EmitOutput(data []byte) {
	f.readCh <- append([]byte(nil), data...)
}
