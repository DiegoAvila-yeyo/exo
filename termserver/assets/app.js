(function () {
  const token = metaContent("nucleo-token");
  const csrf = metaContent("nucleo-csrf");

  const refs = {
    sessionList: document.getElementById("session-list"),
    createForm: document.getElementById("create-session-form"),
    workdirInput: document.getElementById("workdir-input"),
    nameInput: document.getElementById("name-input"),
    refreshButton: document.getElementById("refresh-sessions"),
    closeButton: document.getElementById("close-session-button"),
    takeControlButton: document.getElementById("take-control-button"),
    connectionIndicator: document.getElementById("connection-indicator"),
    ownerIndicator: document.getElementById("owner-indicator"),
    banner: document.getElementById("banner"),
    terminalRoot: document.getElementById("terminal"),
    terminalOverlay: document.getElementById("terminal-overlay"),
    overlayTitle: document.getElementById("overlay-title"),
    overlayCopy: document.getElementById("overlay-copy"),
    chatLog: document.getElementById("chat-log"),
    chatForm: document.getElementById("chat-form"),
    chatInput: document.getElementById("chat-input"),
    chatSubmitButton: document.getElementById("chat-submit-button"),
    chatStatusIndicator: document.getElementById("chat-status-indicator"),
    chatFeedback: document.getElementById("chat-feedback"),
    sessionCloseBanner: document.getElementById("session-close-banner"),
    sessionCloseText: document.getElementById("session-close-text"),
    sessionCloseButton: document.getElementById("session-close-button"),
    sessionCloseDismiss: document.getElementById("session-close-dismiss"),
    chatPanel: document.getElementById("chat-panel"),
    newChatButton: document.getElementById("new-chat-button"),
    approvalBanner: document.getElementById("approval-banner"),
    approvalSession: document.getElementById("approval-session"),
    approvalPrompt: document.getElementById("approval-prompt"),
    approvalDetail: document.getElementById("approval-detail"),
    approveButton: document.getElementById("approve-button"),
    denyButton: document.getElementById("deny-button"),
    suggestionChips: Array.prototype.slice.call(document.querySelectorAll(".suggestion-chip")),
    chatSessionList: document.getElementById("chat-session-list"),
    chatSectionToggle: document.getElementById("chat-section-toggle"),
    chatPowerToggle: document.getElementById("chat-power-toggle"),
    projectSectionToggle: document.getElementById("project-section-toggle"),
    projectRootName: document.getElementById("project-root-name"),
    projectRootAdd: document.getElementById("project-root-add"),
    projectList: document.getElementById("project-list"),
    homeView: document.getElementById("home-view"),
    planningNavItem: document.getElementById("planning-nav-item"),
    planningView: document.getElementById("planning-view"),
    planningListScreen: document.getElementById("planning-list-screen"),
    planningList: document.getElementById("planning-list"),
    planningNewButton: document.getElementById("planning-new-button"),
    planningBoardScreen: document.getElementById("planning-board-screen"),
    planningBackButton: document.getElementById("planning-back-button"),
    planningBreadcrumbName: document.getElementById("planning-breadcrumb-name"),
    planningBoardSelect: document.getElementById("planning-board-select"),
    planningNewBoardButton: document.getElementById("planning-new-board-button"),
    planningNotesToggle: document.getElementById("planning-notes-toggle"),
    planningNotesPanel: document.getElementById("planning-notes-panel"),
    planningNotesList: document.getElementById("planning-notes-list"),
    planningCanvasPlaceholder: document.getElementById("planning-canvas-placeholder"),
    planningChatSlot: document.getElementById("planning-chat-slot"),
    canvasView: document.getElementById("canvas-view"),
    canvasDiagramArea: document.getElementById("canvas-diagram-area"),
    lowerChatLog: document.getElementById("lower-chat-log"),
    lowerChatForm: document.getElementById("lower-chat-form"),
    lowerChatInput: document.getElementById("lower-chat-input"),
    canvasFolderMarker: document.getElementById("canvas-folder-marker"),
    canvasFolderBackButton: document.getElementById("canvas-folder-back-button"),
    canvasEmptyState: document.getElementById("canvas-empty-state"),
    canvasObjects: document.getElementById("canvas-objects"),
    canvasSuggestBanner: document.getElementById("canvas-suggest-banner"),
    canvasSuggestText: document.getElementById("canvas-suggest-text"),
    canvasSuggestButton: document.getElementById("canvas-suggest-button"),
    canvasSuggestDismiss: document.getElementById("canvas-suggest-dismiss"),
    canvasObjectPanel: document.getElementById("canvas-object-panel"),
    canvasObjectPanelBackdrop: document.getElementById("canvas-object-panel-backdrop"),
    canvasObjectPanelTitle: document.getElementById("canvas-object-panel-title"),
    canvasObjectActivationToggle: document.getElementById("canvas-object-activation-toggle"),
    canvasObjectPanelClose: document.getElementById("canvas-object-panel-close"),
    canvasObjectPayloadInput: document.getElementById("canvas-object-payload-input"),
    canvasObjectSaveButton: document.getElementById("canvas-object-save-button"),
    canvasObjectSaveFeedback: document.getElementById("canvas-object-save-feedback"),
    canvasMinichatLog: document.getElementById("canvas-minichat-log"),
    canvasMinichatForm: document.getElementById("canvas-minichat-form"),
    canvasMinichatInput: document.getElementById("canvas-minichat-input"),
  };

  const CHAT_SESSION_STORAGE_KEY = "exo.activeChatSessionId";
  const CHAT_SECTION_COLLAPSED_KEY = "exo.chatSectionCollapsed";
  const ACTIVE_PROJECT_KEY = "exo.activeProject";

  // Round 3: per-tab identity for navigate events. sessionStorage (not
  // localStorage) so each tab gets its own — two tabs on the same chat
  // session must not both jump when only one of them asked to navigate.
  const CLIENT_ID_STORAGE_KEY = "exo.clientId";
  function getOrCreateClientId() {
    let id = window.sessionStorage.getItem(CLIENT_ID_STORAGE_KEY);
    if (!id) {
      id = "client-" + Date.now().toString(36) + "-" + Math.random().toString(36).slice(2);
      window.sessionStorage.setItem(CLIENT_ID_STORAGE_KEY, id);
    }
    return id;
  }
  const clientId = getOrCreateClientId();

  function newTurnId() {
    return "turn-" + Date.now().toString(36) + "-" + Math.random().toString(36).slice(2);
  }

  const state = {
    sessions: [],
    activeSessionId: null,
    socket: null,
    reconnectTimer: null,
    reconnectAttempt: 0,
    intentionalClose: false,
    connectionState: "disconnected",
    owner: "",
    epoch: 0,
    chatSource: null,
    chatReconnectTimer: null,
    chatReconnectAttempt: 0,
    chatStatus: "idle",
    // 2026-08-21 UI design round: true from the moment a message is sent
    // until the first real "output" chunk (or the turn ending) arrives —
    // renderChatLog shows a "thinking" row while it's true. Needed more
    // than ever after the chat_output_filter.go fix: the browser now sees
    // *nothing* until "=== FINAL ===", which can take a while, so the
    // silent gap this covers got longer, not shorter.
    chatThinking: false,
    pendingApproval: null,
    chatSessions: [],
    activeChatSessionId: null,
    // The workspace's current project — independent of whichever chat is
    // open. Set from the sidebar, sent on every message, and used to filter
    // "Recent" down to that project's sessions.
    activeProjectPath: null,
    activeProjectName: null,
    // 2026-08-21 UI design round: the global "Recent" list is hidden now —
    // sessions are grouped under each project instead, Claude-Code-desktop
    // style (project name as a group header, its sessions listed directly
    // below, collapsible via a chevron), plus one more group for the root
    // itself (sessions with no project_path). Keyed by project.path, or
    // buildGroup's ROOT_SESSIONS_KEY ("") for the root group — undefined
    // (never toggled) falls back to that group's own defaultExpanded (true
    // for real projects, false for root: this repo alone had 194
    // project-less legacy sessions, so opening that flat by default would
    // bury every real project below it). Independent of activeProjectPath:
    // collapsing/expanding a group doesn't change which project is active,
    // and opening a session from any group doesn't either (same rule
    // openChatSession already followed).
    expandedProjectSessions: {},
    projects: [], // last-loaded /api/projects list, kept so renderProjectList can re-run without refetching
    // --- Planning (Round 1) ---
    view: "home", // "home" | "planning-list" | "planning-board"
    plannings: [],
    activePlanning: null, // full Planning object (boards + knowledge) once opened
    activeBoardId: null,
    // Round 3: turn_id of this tab's most recent submit — a `navigate`
    // event carrying any other turn_id is stale (e.g. arrived late after a
    // reconnect, after the user already moved on) and must be ignored.
    lastTurnId: null,
    // --- Canvas ---
    canvas: null, // last-fetched ProjectCanvas (or null before first fetch)
    activeCanvasObjectId: null, // which object the floating panel currently has open
    pendingCanvasSuggestion: null, // {object_id, name} from the most recent canvas_suggest SSE event
    // 2026-08-20 UI design round: the diagram area starts collapsed behind
    // the "diagramas" folder marker; clicking it reveals the materialized
    // diagrams as small file-style thumbnails, clicking one of those
    // expands it to the full diagram. Two levels of "back": expanded ->
    // grid, grid -> closed. Purely view-state, doesn't touch canvasstore
    // data.
    canvasFolderOpen: false,
    expandedCanvasObjectId: null,
    // --- Sesiones (session recall) ---
    // Whether the currently open chat session is closed — set from
    // openChatSession's own load and from the close endpoint's response.
    // Closed is terminal: the composer is disabled and no new /api/chat
    // POST is attempted while this is true (server also rejects it).
    activeChatSessionClosed: false,
    // true once this session's own "usage" SSE event crossed
    // closeSessionWarnThreshold — drives the persistent banner above
    // chat-log. Dismissing it just hides it for this session; it does not
    // come back until another "usage" event re-crosses the threshold.
    sessionCloseBannerDismissed: false,
  };

  // Where #chat-panel lives when no Planning board is open — captured once
  // at boot so it can be re-parented back exactly where it started.
  const homeChatAnchor = document.createComment("chat-panel-home-anchor");
  refs.chatPanel.parentNode.insertBefore(homeChatAnchor, refs.chatPanel);

  const terminal = new Terminal({
    cursorBlink: true,
    fontFamily: '"SFMono-Regular", "Menlo", monospace',
    fontSize: 14,
    lineHeight: 1.18,
    convertEol: true,
    scrollback: 5000,
    theme: {
      background: "#101010",
      foreground: "#f8e5cf",
      cursor: "#ffb26b",
      black: "#101010",
      red: "#ff7f50",
      green: "#7bd88f",
      yellow: "#f2c14f",
      blue: "#80c7ff",
      magenta: "#d5a6ff",
      cyan: "#74d7ec",
      white: "#f7f1e8",
    },
  });
  const fitAddon = new FitAddon.FitAddon();
  const inputEncoder = new TextEncoder();
  terminal.loadAddon(fitAddon);
  terminal.open(refs.terminalRoot);

  let resizeDebounce = null;

  terminal.onData(function (data) {
    if (!state.activeSessionId) {
      showBanner("Select or create a session before typing.");
      return;
    }
    if (!canWrite()) {
      if (state.connectionState === "ready" && state.owner === "agent") {
        showBanner("Agent has control. Click Take control before typing or pasting.");
      } else if (state.connectionState === "session-lost") {
        showBanner("This session no longer exists on the server.");
      } else {
        showBanner("Terminal input is read-only until the connection is ready.");
      }
      updateOverlay();
      return;
    }
    if (state.socket && state.socket.readyState === WebSocket.OPEN) {
      state.socket.send(inputEncoder.encode(data));
    }
  });

  window.addEventListener("resize", scheduleFitAndResize);
  new ResizeObserver(scheduleFitAndResize).observe(refs.terminalRoot);

  refs.refreshButton.addEventListener("click", function () {
    refreshSessions();
  });

  refs.createForm.addEventListener("submit", async function (event) {
    event.preventDefault();
    const workdir = refs.workdirInput.value.trim();
    const name = refs.nameInput.value.trim();
    if (!workdir) {
      showBanner("A working directory is required to create a session.");
      return;
    }
    setCreateDisabled(true);
    try {
      const created = await fetchJSON("/api/sessions?csrf_token=" + encodeURIComponent(csrf), {
        method: "POST",
        headers: {"Content-Type": "application/json"},
        body: JSON.stringify({workdir: workdir, name: name}),
      });
      refs.nameInput.value = "";
      showBanner("Session created.");
      await refreshSessions(created.id);
      activateSession(created.id);
    } catch (error) {
      showBanner(error.message || "Could not create session.");
    } finally {
      setCreateDisabled(false);
    }
  });

  refs.closeButton.addEventListener("click", async function () {
    if (!state.activeSessionId) {
      return;
    }
    const closingID = state.activeSessionId;
    disconnectSocket(false);
    try {
      await fetchJSON("/api/sessions/" + encodeURIComponent(closingID) + "/close?csrf_token=" + encodeURIComponent(csrf), {
        method: "POST",
      }, true);
      showBanner("Session closed.");
      state.activeSessionId = null;
      await refreshSessions();
      if (state.sessions.length > 0) {
        activateSession(state.sessions[0].id);
      } else {
        resetTerminal("No sessions yet.\r\nCreate one from the left panel to begin.");
        setConnectionState("disconnected");
        state.owner = "";
        updateUI();
      }
    } catch (error) {
      showBanner(error.message || "Could not close session.");
      await refreshSessions(closingID);
      activateSession(closingID);
    }
  });

  refs.takeControlButton.addEventListener("click", function () {
    if (!state.socket || state.socket.readyState !== WebSocket.OPEN || !state.activeSessionId) {
      return;
    }
    refs.takeControlButton.disabled = true;
    state.socket.send(JSON.stringify({type: "takeover", owner: "human"}));
    showBanner("Takeover requested. Waiting for server confirmation.");
  });

  refs.chatForm.addEventListener("submit", async function (event) {
    event.preventDefault();
    const message = refs.chatInput.value.trim();
    if (!message) {
      return;
    }
    if (state.activeChatSessionClosed) {
      setChatFeedback("This session is closed. Start a new chat to continue.");
      return;
    }
    setChatFeedback("");
    setChatSubmitting(true);
    try {
      const body = {message: message};
      if (state.activeChatSessionId) {
        body.session_id = state.activeChatSessionId;
      }
      if (state.activeProjectPath) {
        body.project_path = state.activeProjectPath;
      }
      // Unlike project_path, planning_id/board_id are always sent, every
      // submission — real values while docked in a Board, explicit ""/""
      // everywhere else. The backend treats omitted vs. explicit "" as
      // different things (see resolvePlanningContext server-side), so never
      // just leave these out.
      // Sending planning_id alone (board_id "") is valid and intentional:
      // it's "in this Planning, no Board open yet" — e.g. a Planning with
      // no Boards at all, where planning_create_board still needs to work.
      // board_id is only ever sent alongside a non-empty planning_id.
      const inPlanning = state.view === "planning-board" && state.activePlanning;
      body.planning_id = inPlanning ? state.activePlanning.id : "";
      body.board_id = inPlanning && state.activeBoardId ? state.activeBoardId : "";
      // Round 3: client_id identifies this tab, turn_id this specific
      // submit — both generated here, sent every time, never derived
      // server-side. A `navigate` SSE event only applies if both match.
      body.client_id = clientId;
      const turnId = newTurnId();
      body.turn_id = turnId;
      state.lastTurnId = turnId;
      const response = await postJSON("/api/chat?csrf_token=" + encodeURIComponent(csrf), body);
      if (response && response.session_id && response.session_id !== state.activeChatSessionId) {
        setActiveChatSession(response.session_id);
      }
      state.chatThinking = true;
      appendChatEntry("You: " + message, "system");
      refs.chatInput.value = "";
    } catch (error) {
      state.chatThinking = false;
      if (error && error.message === "busy") {
        setChatFeedback("The agent is already working on another request.");
      } else {
        setChatFeedback(error.message || "Could not send chat message.");
      }
    } finally {
      setChatSubmitting(false);
    }
  });

  refs.approveButton.addEventListener("click", function () {
    respondToApproval(true);
  });

  refs.denyButton.addEventListener("click", function () {
    respondToApproval(false);
  });

  refs.newChatButton.addEventListener("click", function () {
    // "New chat" is also exo's only way back to Home today — leaving
    // Planning must restore the normal Chat/Home layout, and starting a
    // fresh conversation is exactly the moment a user expects that.
    showHomeView();
    startNewChat();
  });

  refs.planningNavItem.addEventListener("click", function () {
    showPlanningListView();
  });

  refs.planningNewButton.addEventListener("click", async function () {
    const name = window.prompt("Name this planning:");
    if (!name || !name.trim()) {
      return;
    }
    try {
      const created = await postJSON("/api/plannings?csrf_token=" + encodeURIComponent(csrf), {name: name.trim()});
      await openPlanning(created.id);
    } catch (error) {
      window.alert(error.message || "Could not create planning.");
    }
  });

  refs.planningBackButton.addEventListener("click", function () {
    showPlanningListView();
  });

  refs.planningNewBoardButton.addEventListener("click", async function () {
    if (!state.activePlanning) {
      return;
    }
    const name = window.prompt("Name this board:");
    if (!name || !name.trim()) {
      return;
    }
    try {
      await postJSON(
        "/api/plannings/" + encodeURIComponent(state.activePlanning.id) + "/boards?csrf_token=" + encodeURIComponent(csrf),
        {name: name.trim()}
      );
      await openPlanning(state.activePlanning.id, {keepBoard: false});
    } catch (error) {
      window.alert(error.message || "Could not create board.");
    }
  });

  refs.planningBoardSelect.addEventListener("change", function () {
    switchBoard(refs.planningBoardSelect.value);
  });

  refs.planningNotesToggle.addEventListener("click", function () {
    const expanded = refs.planningNotesToggle.getAttribute("aria-expanded") === "true";
    setNotesPanelExpanded(!expanded);
  });

  refs.chatSectionToggle.addEventListener("click", function () {
    const expanded = refs.chatSectionToggle.getAttribute("aria-expanded") !== "false";
    setChatSectionExpanded(!expanded);
  });

  // Standalone on/off switch next to Send — visual only for now, not wired
  // to any behavior yet.
  refs.chatPowerToggle.addEventListener("click", function () {
    const on = refs.chatPowerToggle.getAttribute("aria-checked") === "true";
    refs.chatPowerToggle.setAttribute("aria-checked", on ? "false" : "true");
  });

  refs.projectSectionToggle.addEventListener("click", function () {
    const expanded = refs.projectSectionToggle.getAttribute("aria-expanded") === "true";
    setProjectSectionExpanded(!expanded);
    if (!expanded) {
      loadProjectList();
    }
  });

  // Root-level "+" (2026-08-21 round): same action as the top "New chat"
  // button — no project picker of its own, since one click on any
  // project's own "+" already covers "new chat in a specific project".
  refs.projectRootAdd.addEventListener("click", function () {
    showHomeView();
    startNewChat();
  });

  refs.suggestionChips.forEach(function (chip) {
    chip.addEventListener("click", function () {
      const prompt = chip.getAttribute("data-prompt") || "";
      refs.chatInput.value = prompt;
      refs.chatInput.focus();
      refs.chatInput.setSelectionRange(prompt.length, prompt.length);
    });
  });

  boot();

  async function boot() {
    resetTerminal("Loading Exo terminal UI...\r\n");
    fitAddon.fit();
    renderChatLog();
    connectChatStream();
    await refreshSessions();
    if (state.sessions.length > 0) {
      activateSession(state.sessions[0].id);
    } else {
      resetTerminal("No sessions available.\r\nCreate one from the sidebar.");
      updateUI();
    }
    // Restore the active project before the chat session list renders, so
    // "Recent" filters correctly on first paint instead of flashing
    // unfiltered then re-rendering.
    restoreActiveProject();
    await refreshCanvasView();
    await restoreChatSession();
  }

  // --- chat sessions (persisted, named conversations shown in the sidebar) ---

  function setChatSectionExpanded(expanded) {
    refs.chatSectionToggle.setAttribute("aria-expanded", expanded ? "true" : "false");
    window.localStorage.setItem(CHAT_SECTION_COLLAPSED_KEY, expanded ? "" : "1");
  }

  async function restoreChatSession() {
    setChatSectionExpanded(window.localStorage.getItem(CHAT_SECTION_COLLAPSED_KEY) !== "1");
    await refreshChatSessions();
    const savedID = window.localStorage.getItem(CHAT_SESSION_STORAGE_KEY);
    if (savedID && state.chatSessions.some(function (session) { return session.id === savedID; })) {
      await openChatSession(savedID);
    }
  }

  async function refreshChatSessions() {
    try {
      const sessions = await fetchJSON("/api/chat/sessions", {method: "GET"});
      state.chatSessions = Array.isArray(sessions) ? sessions : [];
      renderChatSessionList();
      // "Recent" itself is hidden (2026-08-21 round), but each project's
      // "Sessions" tab reads from the same state.chatSessions — keep it in
      // sync too, or an expanded tab would go stale after a new turn.
      renderProjectList();
    } catch (error) {
      // Non-fatal: the sidebar just stays empty/stale. The main chat still works.
    }
  }

  // Filters to the active project's sessions when one is set — otherwise
  // shows everything, same as before projects existed (including old
  // sessions that have no project_path at all).
  function renderChatSessionList() {
    refs.chatSessionList.innerHTML = "";
    const visible = state.activeProjectPath
      ? state.chatSessions.filter(function (session) { return session.project_path === state.activeProjectPath; })
      : state.chatSessions;

    if (visible.length === 0) {
      const empty = document.createElement("p");
      empty.className = "sidebar-placeholder";
      empty.textContent = state.activeProjectPath
        ? "No chats in " + (state.activeProjectName || "this project") + " yet."
        : "Your chats will appear here.";
      refs.chatSessionList.appendChild(empty);
      return;
    }

    visible.forEach(function (session) {
      const item = document.createElement("button");
      item.type = "button";
      item.className = "chat-session-item" + (session.id === state.activeChatSessionId ? " active" : "");
      item.textContent = session.title || "New chat";
      item.title = session.title || "New chat";
      item.addEventListener("click", function () {
        openChatSession(session.id);
      });
      refs.chatSessionList.appendChild(item);
    });
  }

  // "New chat" only clears the local view — it does NOT create a session on
  // the server. A session is created lazily on the first message actually
  // sent (see the chat form submit handler), so clicking "New chat" and
  // never typing anything leaves no empty session behind. The active
  // project is untouched — a new chat inherits whatever project is
  // currently active in the sidebar.
  function startNewChat() {
    setActiveChatSession(null);
    resetChatView();
  }

  // Opening a past chat loads its transcript only — it deliberately does
  // NOT change the active project. The project is workspace-wide state now;
  // browsing your history shouldn't side-swap what you're working on.
  async function openChatSession(id) {
    try {
      const session = await fetchJSON("/api/chat/sessions/" + encodeURIComponent(id), {method: "GET"});
      setActiveChatSession(session.id);
      refs.chatLog.dataset.entries = JSON.stringify(session.entries || []);
      refs.chatInput.value = "";
      renderChatLog();
      renderChatSessionList();
      renderProjectList(); // updates the "active" mark inside its Sessions tab too
      // Closed is terminal (Sesiones) — reopening a closed session shows its
      // transcript read-only; the composer stays disabled until a new
      // session is started. The banner belongs only to the session that
      // triggered it, so switching sessions always resets its state.
      state.activeChatSessionClosed = session.status === "closed";
      state.sessionCloseBannerDismissed = false;
      hideSessionCloseBanner();
      updateChatComposerDisabled();
    } catch (error) {
      setChatFeedback(error.message || "Could not open that chat.");
    }
  }

  // Disables the composer while the active session is closed — the human's
  // path to continue is a new session (New chat), never posting into this
  // one. Server-side, /api/chat rejects it too (termserver/chat.go); this
  // is the client-side half of the same rule.
  function updateChatComposerDisabled() {
    const closed = state.activeChatSessionClosed;
    refs.chatInput.disabled = closed;
    refs.chatSubmitButton.disabled = closed;
    refs.chatInput.placeholder = closed
      ? "This session is closed — start a new chat to continue."
      : "Ask the agent to inspect, run, or explain something";
  }

  function setActiveChatSession(id) {
    state.activeChatSessionId = id;
    if (id) {
      window.localStorage.setItem(CHAT_SESSION_STORAGE_KEY, id);
    } else {
      window.localStorage.removeItem(CHAT_SESSION_STORAGE_KEY);
    }
    renderChatSessionList();
  }

  // --- projects (sidebar workspace switcher) ---
  //
  // The active project is app-wide state, not tied to whichever chat is
  // open: picking one here filters "Recent" to that project's sessions and
  // gets sent as project_path on the next message, regardless of which
  // chat you're in. Opening a past chat does NOT change the active
  // project — see openChatSession, which deliberately leaves it alone.

  function setProjectSectionExpanded(expanded) {
    refs.projectSectionToggle.setAttribute("aria-expanded", expanded ? "true" : "false");
    refs.projectList.hidden = !expanded;
  }

  // Every project is a direct child of the same root (the server scans one
  // fixed s.projectRoot — the home dir, see backend.go — and that root
  // itself is never sent over the API, only the projects inside it). The
  // root's own name is just the parent directory of any project's path, so
  // it's derived here instead of asking the server for something new.
  function rootFolderNameFromProjectPath(path) {
    const trimmed = path.replace(/\/+$/, "");
    const parent = trimmed.slice(0, trimmed.lastIndexOf("/"));
    const name = parent.slice(parent.lastIndexOf("/") + 1);
    return name || "Projects";
  }

  async function loadProjectList() {
    try {
      const list = await fetchJSON("/api/projects", {method: "GET"});
      state.projects = Array.isArray(list) ? list : [];
    } catch (error) {
      state.projects = [];
    }
    if (state.projects.length > 0) {
      refs.projectRootName.textContent = rootFolderNameFromProjectPath(state.projects[0].path);
    }
    renderProjectList();
  }

  // Builds a .chat-session-list of session buttons (dot + bold title,
  // active one highlighted) — shared by the root's own sessions (below)
  // and every project group, so both look and behave identically.
  function buildSessionsListElement(sessions) {
    const sessionsList = document.createElement("div");
    sessionsList.className = "chat-session-list";
    // Sesiones: closed sessions sort below open ones within this group —
    // metadata, not something competing for attention with active work.
    // Array.prototype.sort is stable in every browser this app targets, so
    // sessions keep their existing (already most-recently-updated-first)
    // order within each of the two buckets.
    const sorted = sessions.slice().sort(function (a, b) {
      return (a.status === "closed" ? 1 : 0) - (b.status === "closed" ? 1 : 0);
    });
    sorted.forEach(function (session) {
      const closed = session.status === "closed";
      const sessionItem = document.createElement("button");
      sessionItem.type = "button";
      sessionItem.className = "chat-session-item" +
        (session.id === state.activeChatSessionId ? " active" : "") +
        (closed ? " chat-session-item--closed" : "");
      sessionItem.innerHTML = "<span class=\"chat-session-item__dot\" aria-hidden=\"true\">○</span><span></span>";
      sessionItem.querySelector("span:last-child").textContent = session.title || "New chat";
      if (closed) {
        const badge = document.createElement("span");
        badge.className = "chat-session-item__badge";
        badge.textContent = "Closed";
        sessionItem.appendChild(badge);
      }
      sessionItem.title = session.title || "New chat";
      sessionItem.addEventListener("click", function () {
        openChatSession(session.id);
      });
      sessionsList.appendChild(sessionItem);
    });
    return sessionsList;
  }

  // Sentinel key for the root's own group in state.expandedProjectSessions
  // — real projects are keyed by their (always non-empty) path, so "" can
  // never collide with one.
  const ROOT_SESSIONS_KEY = "";

  // One collapsible group: header (chevron + name, click selects + toggles)
  // with an optional "+", and its sessions listed beneath when expanded.
  // Shared by the root's own ungrouped sessions and every real project, so
  // both look and behave identically — defaultExpanded is the only real
  // difference between the two call sites (see renderProjectList).
  function buildGroup(key, name, sessions, options) {
    const opts = options || {};
    const group = document.createElement("div");
    group.className = "project-group";

    // undefined (never toggled) falls back to defaultExpanded.
    const stored = state.expandedProjectSessions[key];
    const expanded = stored === undefined ? !!opts.defaultExpanded : stored;

    const header = document.createElement("div");
    header.className = "project-group-header";

    const toggle = document.createElement("button");
    toggle.type = "button";
    toggle.className = "project-group-toggle" + (opts.active ? " active" : "");
    toggle.setAttribute("aria-expanded", expanded ? "true" : "false");
    toggle.innerHTML =
      "<span class=\"project-group-chevron\" aria-hidden=\"true\">⌄</span>" +
      "<span class=\"project-group-name\"></span>";
    toggle.querySelector(".project-group-name").textContent = name;
    if (opts.title) {
      toggle.title = opts.title;
    }
    toggle.addEventListener("click", function () {
      if (opts.onSelect) {
        opts.onSelect();
      }
      state.expandedProjectSessions[key] = !expanded;
      renderProjectList();
    });
    header.appendChild(toggle);

    if (opts.onAdd) {
      const addButton = document.createElement("button");
      addButton.type = "button";
      addButton.className = "project-group-add";
      addButton.textContent = "+";
      addButton.setAttribute("aria-label", "New chat in " + name);
      addButton.title = "New chat in " + name;
      addButton.addEventListener("click", function (event) {
        event.stopPropagation(); // don't also trigger the header's toggle
        state.expandedProjectSessions[key] = true;
        opts.onAdd();
        renderProjectList();
      });
      header.appendChild(addButton);
    }

    group.appendChild(header);

    if (expanded) {
      const sessionsList = buildSessionsListElement(sessions);
      sessionsList.classList.add("project-group-sessions");
      group.appendChild(sessionsList);
    }

    return group;
  }

  // 2026-08-21: redesigned to match the Claude Code desktop sidebar the
  // human pointed to — each project is a group header (name + a "+" to
  // start a new chat there) with its sessions listed directly beneath,
  // collapsible via a chevron but expanded by default (no extra click
  // needed to see them, unlike the previous "Sessions" sub-tab). The root
  // itself gets its own group too, for sessions with no project_path at
  // all (created before a project was ever picked) — collapsed by default,
  // unlike real projects: this repo alone had 194 of them, and showing
  // that flat list open by default would bury every real project below a
  // wall of legacy sessions. Reads from
  // state.projects/state.chatSessions/state.expandedProjectSessions rather
  // than taking params so any caller (a group's chevron, a session
  // refresh) can just call renderProjectList() for a consistent re-render.
  function renderProjectList() {
    refs.projectList.innerHTML = "";

    const rootSessions = state.chatSessions.filter(function (session) {
      return !session.project_path;
    });
    if (rootSessions.length > 0) {
      refs.projectList.appendChild(buildGroup(
        ROOT_SESSIONS_KEY,
        "Sin proyecto (" + rootSessions.length + ")",
        rootSessions,
        {defaultExpanded: false}
      ));
    }

    const list = state.projects;
    if (list.length === 0) {
      if (rootSessions.length === 0) {
        const empty = document.createElement("p");
        empty.className = "sidebar-placeholder";
        empty.textContent = "No projects found.";
        refs.projectList.appendChild(empty);
      }
      return;
    }
    list.forEach(function (project) {
      const sessions = state.chatSessions.filter(function (session) {
        return session.project_path === project.path;
      });
      refs.projectList.appendChild(buildGroup(project.path, project.name, sessions, {
        defaultExpanded: true,
        active: project.path === state.activeProjectPath,
        title: project.path,
        // A single click both selects this project (as before) and
        // toggles its group open/closed — the reference design doesn't
        // separate those two actions into different controls.
        onSelect: function () { setActiveProject(project.name, project.path); },
        onAdd: function () {
          setActiveProject(project.name, project.path);
          startNewChat();
        },
      }));
    });
  }

  // Sets the workspace's active project — path is what gets sent to the
  // backend on the next message; name is just for display. Persisted so a
  // page refresh doesn't lose the pick. Re-filters "Recent" immediately
  // from already-loaded session data, no extra fetch needed.
  function setActiveProject(name, path) {
    state.activeProjectPath = path || null;
    state.activeProjectName = name || null;
    if (path) {
      window.localStorage.setItem(ACTIVE_PROJECT_KEY, JSON.stringify({name: name, path: path}));
    } else {
      window.localStorage.removeItem(ACTIVE_PROJECT_KEY);
    }
    renderChatSessionList();
    refreshCanvasView();
  }

  function restoreActiveProject() {
    const raw = window.localStorage.getItem(ACTIVE_PROJECT_KEY);
    if (!raw) {
      return;
    }
    try {
      const saved = JSON.parse(raw);
      if (saved && saved.path) {
        state.activeProjectPath = saved.path;
        state.activeProjectName = saved.name;
      }
    } catch (error) {
      window.localStorage.removeItem(ACTIVE_PROJECT_KEY);
    }
  }

  // --- planning (Round 1: list + board shell, reusing the existing chat) ---
  //
  // No frontend router: three mutually-exclusive views toggled by hiding/
  // showing DOM sections — home-view (chat/terminal, unchanged), and
  // planning-view's two screens (list, board). #chat-panel is a single
  // shared node re-parented between home-view and the board screen's chat
  // slot, never duplicated, so it keeps its live session/log intact
  // wherever it currently lives.

  function setActiveNav(view) {
    refs.planningNavItem.classList.toggle("active", view !== "home");
  }

  function showHomeView() {
    state.view = "home";
    undockChatToHome();
    refs.homeView.hidden = false;
    refs.planningView.hidden = true;
    setActiveNav("home");
  }

  function showPlanningListView() {
    state.view = "planning-list";
    undockChatToHome();
    refs.homeView.hidden = true;
    refs.planningView.hidden = false;
    refs.planningListScreen.hidden = false;
    refs.planningBoardScreen.hidden = true;
    setActiveNav("planning-list");
    refreshPlanningList();
  }

  async function refreshPlanningList() {
    refs.planningList.innerHTML = "";
    const loading = document.createElement("p");
    loading.className = "sidebar-placeholder";
    loading.textContent = "Loading…";
    refs.planningList.appendChild(loading);
    try {
      const list = await fetchJSON("/api/plannings", {method: "GET"});
      state.plannings = Array.isArray(list) ? list : [];
      renderPlanningList();
    } catch (error) {
      refs.planningList.innerHTML = "";
      const empty = document.createElement("p");
      empty.className = "sidebar-placeholder";
      empty.textContent = error.message || "Could not load plannings.";
      refs.planningList.appendChild(empty);
    }
  }

  function renderPlanningList() {
    refs.planningList.innerHTML = "";
    if (state.plannings.length === 0) {
      const empty = document.createElement("p");
      empty.className = "sidebar-placeholder";
      empty.textContent = "No plannings yet. Create one to start designing.";
      refs.planningList.appendChild(empty);
      return;
    }
    state.plannings.forEach(function (planning) {
      const item = document.createElement("button");
      item.type = "button";
      item.className = "planning-list-item";
      const name = document.createElement("span");
      name.className = "planning-list-item-name";
      name.textContent = planning.name;
      const meta = document.createElement("span");
      meta.className = "planning-list-item-meta";
      const boardCount = planning.board_count || 0;
      meta.textContent = boardCount + (boardCount === 1 ? " board" : " boards");
      item.appendChild(name);
      item.appendChild(meta);
      item.addEventListener("click", function () {
        openPlanning(planning.id);
      });
      refs.planningList.appendChild(item);
    });
  }

  // Loads a Planning and opens its board screen. keepBoard (default true)
  // tries to stay on the currently selected board id after a refresh (e.g.
  // after creating a new board); pass false to jump to the newest board.
  // options.targetBoardId: land on exactly this board (Round 3 navigate
  // events with a board_id) instead of the keep-previous/newest heuristic.
  // options.forceNoBoard: land planning-only, no matter what boards exist
  // or what was previously selected (Round 3 navigate events with no
  // board_id — planning_open never auto-selects a Board, so the frontend
  // must not "helpfully" pick one either).
  async function openPlanning(planningId, options) {
    options = options || {};
    const forceNoBoard = options.forceNoBoard === true;
    const targetBoardId = options.targetBoardId || null;
    const keepBoard = !forceNoBoard && !targetBoardId && options.keepBoard !== false;
    const previousBoardId = keepBoard ? state.activeBoardId : null;
    try {
      const planning = await fetchJSON("/api/plannings/" + encodeURIComponent(planningId), {method: "GET"});
      state.activePlanning = planning;
      state.view = "planning-board";
      // No explicit "detach" step needed: dockChatIntoBoard's appendChild
      // re-parents #chat-panel from wherever it currently lives (including
      // another board's slot) — a DOM node only ever has one parent.
      // Calling undockChatToHome() here would also clear state.activePlanning
      // right after we just set it, per its contract below.
      refs.homeView.hidden = true;
      refs.planningView.hidden = false;
      refs.planningListScreen.hidden = true;
      refs.planningBoardScreen.hidden = false;
      setActiveNav("planning-board");

      refs.planningBreadcrumbName.textContent = planning.name;

      const boards = planning.boards || [];
      let nextBoardId;
      if (forceNoBoard) {
        nextBoardId = null;
      } else if (targetBoardId) {
        nextBoardId = boards.some(function (b) { return b.id === targetBoardId; }) ? targetBoardId : null;
      } else {
        const stillExists = previousBoardId && boards.some(function (b) { return b.id === previousBoardId; });
        nextBoardId = stillExists ? previousBoardId : (boards.length > 0 ? boards[boards.length - 1].id : null);
      }
      populateBoardSelect(boards, nextBoardId);
      switchBoard(nextBoardId);

      dockChatIntoBoard();
    } catch (error) {
      window.alert(error.message || "Could not open that planning.");
    }
  }

  // --- Round 3: applying a `navigate` event from the agent ---

  function applyNavigateAction(action) {
    if (action.board_id) {
      openPlanning(action.planning_id, {targetBoardId: action.board_id});
    } else {
      openPlanning(action.planning_id, {forceNoBoard: true});
    }
  }

  // Round 2 post-turn refresh: re-fetch Boards (and Knowledge, if Notes is
  // open) after a chat turn completes, only while actually docked in a
  // Board — a turn finishing on Home or the Planning list has nothing here
  // to refresh.
  async function refreshBoardScreenAfterTurn() {
    if (state.view !== "planning-board" || !state.activePlanning) {
      return;
    }
    try {
      const planning = await fetchJSON("/api/plannings/" + encodeURIComponent(state.activePlanning.id), {method: "GET"});
      state.activePlanning = planning;
      populateBoardSelect(planning.boards || [], state.activeBoardId);
    } catch (error) {
      return; // non-fatal: the board screen just doesn't refresh this turn
    }
    if (refs.planningNotesToggle.getAttribute("aria-expanded") === "true") {
      loadBoardKnowledge();
    }
  }

  function populateBoardSelect(boards, selectedId) {
    refs.planningBoardSelect.innerHTML = "";
    if (boards.length === 0) {
      const option = document.createElement("option");
      option.value = "";
      option.textContent = "No boards yet";
      refs.planningBoardSelect.appendChild(option);
      refs.planningBoardSelect.disabled = true;
      return;
    }
    refs.planningBoardSelect.disabled = false;
    // Landing planning-only (selectedId null/absent) is a real state even
    // when boards exist (Round 3: planning_open never auto-selects a
    // Board) — without an explicit placeholder option, a <select> with no
    // option marked selected falls back to showing its first real option
    // as selected, silently contradicting state.activeBoardId being null.
    if (!selectedId) {
      const placeholder = document.createElement("option");
      placeholder.value = "";
      placeholder.textContent = "Select a board…";
      placeholder.selected = true;
      refs.planningBoardSelect.appendChild(placeholder);
    }
    boards.forEach(function (board) {
      const option = document.createElement("option");
      option.value = board.id;
      option.textContent = board.name;
      option.selected = board.id === selectedId;
      refs.planningBoardSelect.appendChild(option);
    });
  }

  function switchBoard(boardId) {
    state.activeBoardId = boardId || null;
    setNotesPanelExpanded(false);
    if (!boardId) {
      // Two different reasons land here: the Planning genuinely has no
      // Boards yet (select is disabled — Round 1), or it has Boards but
      // none is open (Round 3: planning_open never auto-selects one).
      refs.planningCanvasPlaceholder.querySelector(".planning-canvas-placeholder-title").textContent =
        refs.planningBoardSelect.disabled ? "This planning has no boards yet." : "No board open — pick one above.";
      refs.planningNotesToggle.hidden = true;
      return;
    }
    refs.planningNotesToggle.hidden = false;
    refs.planningCanvasPlaceholder.querySelector(".planning-canvas-placeholder-title").textContent =
      "This board is empty.";
  }

  // --- Knowledge disclosure: on-demand only, never a permanent panel ---

  function setNotesPanelExpanded(expanded) {
    refs.planningNotesToggle.setAttribute("aria-expanded", expanded ? "true" : "false");
    refs.planningNotesPanel.hidden = !expanded;
    if (expanded) {
      loadBoardKnowledge();
    }
  }

  async function loadBoardKnowledge() {
    if (!state.activePlanning || !state.activeBoardId) {
      return;
    }
    refs.planningNotesList.innerHTML = "";
    try {
      const knowledge = await fetchJSON(
        "/api/plannings/" + encodeURIComponent(state.activePlanning.id) +
          "/boards/" + encodeURIComponent(state.activeBoardId) + "/knowledge",
        {method: "GET"}
      );
      renderNotesList(Array.isArray(knowledge) ? knowledge : []);
    } catch (error) {
      const message = document.createElement("p");
      message.className = "sidebar-placeholder";
      message.textContent = error.message || "Could not load notes for this board.";
      refs.planningNotesList.appendChild(message);
    }
  }

  function renderNotesList(entries) {
    refs.planningNotesList.innerHTML = "";
    // rejected/archived stay historical, never in the normal workspace view
    // — same rule Round 1 applied to autoría states generally.
    const visible = entries.filter(function (entry) {
      return entry.author !== "rejected" && entry.author !== "archived";
    });
    if (visible.length === 0) {
      const empty = document.createElement("p");
      empty.className = "sidebar-placeholder";
      empty.textContent = "Nothing on this board yet.";
      refs.planningNotesList.appendChild(empty);
      return;
    }
    visible.forEach(function (entry) {
      const item = document.createElement("div");
      item.className = "planning-note-item";
      const type = document.createElement("span");
      type.className = "planning-note-type";
      type.textContent = entry.type;
      const title = document.createElement("p");
      title.textContent = entry.title;
      title.style.margin = "0";
      item.appendChild(type);
      item.appendChild(title);

      if (entry.author === "ai_suggested") {
        const badge = document.createElement("span");
        badge.className = "planning-note-badge";
        badge.textContent = "AI suggested";
        item.appendChild(badge);

        const actions = document.createElement("div");
        actions.className = "planning-note-actions";
        const acceptButton = document.createElement("button");
        acceptButton.type = "button";
        acceptButton.className = "mini-button";
        acceptButton.textContent = "Accept";
        acceptButton.addEventListener("click", function () {
          resolveKnowledgeSuggestion(entry.id, "accept");
        });
        const rejectButton = document.createElement("button");
        rejectButton.type = "button";
        rejectButton.className = "mini-button";
        rejectButton.textContent = "Reject";
        rejectButton.addEventListener("click", function () {
          resolveKnowledgeSuggestion(entry.id, "reject");
        });
        actions.appendChild(acceptButton);
        actions.appendChild(rejectButton);
        item.appendChild(actions);
      }

      refs.planningNotesList.appendChild(item);
    });
  }

  async function resolveKnowledgeSuggestion(knowledgeId, action) {
    if (!state.activePlanning) {
      return;
    }
    try {
      await postJSON(
        "/api/plannings/" + encodeURIComponent(state.activePlanning.id) +
          "/knowledge/" + encodeURIComponent(knowledgeId) + "/" + action +
          "?csrf_token=" + encodeURIComponent(csrf),
        {}
      );
      loadBoardKnowledge();
    } catch (error) {
      window.alert(error.message || "Could not update that suggestion.");
    }
  }

  // --- moving the one shared chat panel between Home and a Planning board ---

  function dockChatIntoBoard() {
    refs.chatPanel.classList.add("chat-panel--docked");
    refs.planningChatSlot.appendChild(refs.chatPanel);
  }

  // Clears the frontend's Planning context immediately — it cannot clear
  // the backend session's persisted context itself, since it runs on
  // navigation, not necessarily around a chat submission. What it
  // guarantees instead: the very next chat submission (from Home, in
  // whatever session is active) sends the explicit ""/"" pair the backend
  // needs to actually clear that session — see the chat submit handler.
  function undockChatToHome() {
    state.activePlanning = null;
    state.activeBoardId = null;
    if (refs.chatPanel.parentNode === refs.planningChatSlot) {
      refs.chatPanel.classList.remove("chat-panel--docked");
      homeChatAnchor.parentNode.insertBefore(refs.chatPanel, homeChatAnchor.nextSibling);
    }
  }

  function activateSession(sessionID) {
    if (!sessionID) {
      return;
    }
    if (state.activeSessionId === sessionID && state.socket && state.socket.readyState === WebSocket.OPEN) {
      renderSessionList();
      return;
    }
    state.activeSessionId = sessionID;
    state.owner = "";
    state.epoch = 0;
    state.reconnectAttempt = 0;
    clearReconnectTimer();
    disconnectSocket(false);
    resetTerminal("Connecting to " + sessionLabel(activeSession()) + "...\r\n");
    renderSessionList();
    setConnectionState("connecting");
    updateUI();
    connectWebSocket(false);
  }

  async function refreshSessions(preferredID) {
    try {
      const sessions = await fetchJSON("/api/sessions", {method: "GET"});
      state.sessions = Array.isArray(sessions) ? sessions : [];
      if (preferredID && state.sessions.some(function (session) { return session.id === preferredID; })) {
        state.activeSessionId = preferredID;
      } else if (state.activeSessionId && !state.sessions.some(function (session) { return session.id === state.activeSessionId; })) {
        state.activeSessionId = null;
      }
      renderSessionList();
      updateUI();
      return state.sessions;
    } catch (error) {
      showBanner(error.message || "Could not load sessions.");
      return state.sessions;
    }
  }

  function connectWebSocket(isReconnect) {
    const session = activeSession();
    if (!session) {
      setConnectionState("disconnected");
      updateUI();
      return;
    }

    const socket = new WebSocket(wsURL(session.id), ["nucleo-term." + token]);
    socket.binaryType = "arraybuffer";
    state.socket = socket;
    state.intentionalClose = false;
    setConnectionState(isReconnect ? "reconnecting" : "connecting");
    updateUI();

    socket.addEventListener("message", function (event) {
      if (typeof event.data === "string") {
        handleStatusMessage(event.data);
        return;
      }
      const payload = event.data instanceof ArrayBuffer ? new Uint8Array(event.data) : event.data;
      terminal.write(payload);
    });

    socket.addEventListener("close", function () {
      if (state.socket !== socket) {
        return;
      }
      state.socket = null;
      if (state.intentionalClose || !state.activeSessionId) {
        return;
      }
      scheduleReconnect();
    });

    socket.addEventListener("error", function () {
      if (state.socket === socket && state.connectionState !== "reconnecting") {
        setConnectionState("reconnecting");
        updateUI();
      }
    });
  }

  function disconnectSocket(expectReconnect) {
    clearReconnectTimer();
    state.intentionalClose = !expectReconnect;
    if (state.socket) {
      const socket = state.socket;
      state.socket = null;
      socket.close();
    }
  }

  function scheduleReconnect() {
    clearReconnectTimer();
    state.reconnectAttempt += 1;
    setConnectionState("reconnecting");
    updateUI();
    const delay = Math.min(500 * Math.pow(2, state.reconnectAttempt - 1), 4000);
    state.reconnectTimer = window.setTimeout(async function () {
      const sessions = await refreshSessions(state.activeSessionId);
      const sessionStillExists = sessions.some(function (session) {
        return session.id === state.activeSessionId;
      });
      if (!sessionStillExists) {
        setConnectionState("session-lost");
        state.owner = "";
        updateUI();
        resetTerminal("Session lost.\r\nIt no longer exists on the server.");
        return;
      }
      resetTerminal("Reconnecting to " + sessionLabel(activeSession()) + "...\r\n");
      connectWebSocket(true);
    }, delay);
  }

  function clearReconnectTimer() {
    if (state.reconnectTimer) {
      window.clearTimeout(state.reconnectTimer);
      state.reconnectTimer = null;
    }
  }

  function connectChatStream() {
    clearChatReconnectTimer();
    if (state.chatSource) {
      state.chatSource.close();
      state.chatSource = null;
    }

    const source = new EventSource("/api/chat/stream");
    state.chatSource = source;

    source.onmessage = function (event) {
      handleChatEvent(event.data);
    };

    source.onerror = function () {
      if (state.chatSource !== source) {
        return;
      }
      source.close();
      state.chatSource = null;
      scheduleChatReconnect();
    };
  }

  function scheduleChatReconnect() {
    clearChatReconnectTimer();
    state.chatReconnectAttempt += 1;
    const delay = Math.min(500 * Math.pow(2, state.chatReconnectAttempt - 1), 4000);
    setChatFeedback("Chat stream disconnected. Reconnecting...");
    state.chatReconnectTimer = window.setTimeout(function () {
      connectChatStream();
    }, delay);
  }

  function clearChatReconnectTimer() {
    if (state.chatReconnectTimer) {
      window.clearTimeout(state.chatReconnectTimer);
      state.chatReconnectTimer = null;
    }
  }

  function handleChatEvent(raw) {
    let event;
    try {
      event = JSON.parse(raw);
    } catch (error) {
      setChatFeedback("Received malformed chat event.");
      return;
    }

    clearChatReconnectTimer();
    state.chatReconnectAttempt = 0;
    if (refs.chatFeedback.textContent === "Chat stream disconnected. Reconnecting...") {
      setChatFeedback("");
    }

    switch (event.type) {
      case "idle":
        state.chatStatus = "idle";
        state.chatThinking = false;
        updateChatStatus();
        renderChatLog();
        break;
      case "busy":
        state.chatStatus = "busy";
        updateChatStatus();
        break;
      case "output":
        // Real content arrived — the thinking row's job is done.
        state.chatThinking = false;
        appendChatEntry(event.text || "");
        break;
      case "approval":
        showApproval(event);
        break;
      case "navigate":
        // Delivered on the turn's terminal state (success or error — see
        // termserver/chat.go), so this can arrive independently of "done".
        // Every connected tab gets this event (no server-side per-client
        // routing yet); only apply it if it's addressed to this tab and
        // this tab's own most recent submit.
        if (event.client_id === clientId && event.turn_id === state.lastTurnId) {
          applyNavigateAction(event);
        }
        break;
      case "done":
        state.chatStatus = "idle";
        // Safety net for a turn that produced zero "output" events (e.g.
        // an immediate error) — don't leave the thinking row stuck.
        if (state.chatThinking) {
          state.chatThinking = false;
          renderChatLog();
        }
        updateChatStatus();
        // The server only knows a session's final title (derived from the
        // first message) once the turn completes — refresh so the sidebar
        // stops showing "New chat".
        refreshChatSessions();
        // Round 2: the agent may have just created a Board or a Note/
        // Research/Question via the planning_* tools. No SSE event for
        // that — a plain refetch here is enough, gated so a turn that
        // touched nothing doesn't cost two requests for no reason.
        refreshBoardScreenAfterTurn();
        // Canvas state changes (a new draft, a materialize, an edit) are
        // infrequent enough that a full refetch per turn is cheap — keeps
        // the frontend decoupled from agenthost internals rather than
        // needing its own per-mutation SSE event.
        refreshCanvasView();
        break;
      case "canvas_suggest":
        showCanvasSuggestBanner({object_id: event.object_id, name: event.name});
        break;
      case "usage":
        handleUsageEvent(event);
        break;
      default:
        break;
    }
  }

  // --- Sesiones: context-window warning banner ---
  //
  // Only ever reacts to a "usage" event for the session currently open in
  // this tab — every connected tab gets the event (no server-side
  // per-client routing, same as navigate/canvas_suggest), so a background
  // session's usage must never pop a banner over whatever the human is
  // actually looking at.
  const SESSION_CLOSE_WARN_THRESHOLD = 85;

  function handleUsageEvent(event) {
    if (event.session_id !== state.activeChatSessionId) {
      return;
    }
    if (event.context_pct >= SESSION_CLOSE_WARN_THRESHOLD) {
      if (!state.sessionCloseBannerDismissed) {
        showSessionCloseBanner(event.context_pct);
      }
    } else {
      hideSessionCloseBanner();
    }
  }

  function showSessionCloseBanner(pct) {
    refs.sessionCloseText.textContent =
      "This session's context window is " + Math.round(pct) + "% full. Close it to save a summary, then start a new session.";
    refs.sessionCloseBanner.hidden = false;
  }

  function hideSessionCloseBanner() {
    refs.sessionCloseBanner.hidden = true;
  }

  refs.sessionCloseDismiss.addEventListener("click", function () {
    state.sessionCloseBannerDismissed = true;
    hideSessionCloseBanner();
  });

  refs.sessionCloseButton.addEventListener("click", async function () {
    if (!state.activeChatSessionId) {
      return;
    }
    const closingID = state.activeChatSessionId;
    refs.sessionCloseButton.disabled = true;
    try {
      await fetchJSON("/api/chat/sessions/" + encodeURIComponent(closingID) + "/close?csrf_token=" + encodeURIComponent(csrf), {
        method: "POST",
      });
      hideSessionCloseBanner();
      setChatFeedback("Session closed and summarized.");
      // Reopen the same session — now read-only — so the UI (composer
      // disabled, "Closed" badge) reflects its new state immediately,
      // matching how any other closed session already renders.
      await openChatSession(closingID);
      await refreshChatSessions();
    } catch (error) {
      setChatFeedback(error.message || "Could not close session.");
    } finally {
      refs.sessionCloseButton.disabled = false;
    }
  });

  function handleStatusMessage(raw) {
    let message;
    try {
      message = JSON.parse(raw);
    } catch (error) {
      showBanner("Received malformed status message.");
      return;
    }

    switch (message.type) {
      case "ready":
      case "lease":
        state.owner = message.owner || "";
        state.epoch = message.epoch || 0;
        refs.takeControlButton.disabled = false;
        setConnectionState("ready");
        updateUI();
        scheduleFitAndResize();
        if (message.type === "lease" && state.owner === "human") {
          showBanner("You now control this session.");
        }
        break;
      case "ownership_lost":
        showBanner("Control moved to another client. Use Take control to request it back.");
        refs.takeControlButton.disabled = false;
        updateUI();
        break;
      case "error":
        refs.takeControlButton.disabled = false;
        showBanner(message.error || "Terminal error.");
        break;
      default:
        break;
    }
  }

  function scheduleFitAndResize() {
    if (!activeSession()) {
      return;
    }
    window.clearTimeout(resizeDebounce);
    resizeDebounce = window.setTimeout(function () {
      fitAddon.fit();
      if (state.socket && state.socket.readyState === WebSocket.OPEN && state.connectionState === "ready") {
        state.socket.send(JSON.stringify({type: "resize", cols: terminal.cols, rows: terminal.rows}));
      }
    }, 100);
  }

  function canWrite() {
    return state.connectionState === "ready" && state.owner === "human" && state.socket && state.socket.readyState === WebSocket.OPEN;
  }

  function activeSession() {
    return state.sessions.find(function (session) {
      return session.id === state.activeSessionId;
    }) || null;
  }

  function renderSessionList() {
    refs.sessionList.innerHTML = "";
    if (state.sessions.length === 0) {
      const empty = document.createElement("div");
      empty.className = "empty-state";
      empty.textContent = "No sessions yet. Create one to start a shell.";
      refs.sessionList.appendChild(empty);
      return;
    }

    state.sessions.forEach(function (session) {
      const item = document.createElement("div");
      item.className = "session-item" + (session.id === state.activeSessionId ? " active" : "");
      item.tabIndex = 0;
      item.addEventListener("click", function () {
        activateSession(session.id);
      });
      item.addEventListener("keydown", function (event) {
        if (event.key === "Enter" || event.key === " ") {
          event.preventDefault();
          activateSession(session.id);
        }
      });

      const meta = document.createElement("div");
      meta.className = "session-meta";
      meta.innerHTML =
        '<span class="session-name"></span>' +
        '<span class="session-workdir"></span>' +
        '<span class="session-subtle"></span>';
      meta.querySelector(".session-name").textContent = sessionLabel(session);
      meta.querySelector(".session-workdir").textContent = session.workdir;
      meta.querySelector(".session-subtle").textContent = session.status;

      const actions = document.createElement("div");
      actions.className = "session-actions";
      const closeButton = document.createElement("button");
      closeButton.type = "button";
      closeButton.className = "mini-button";
      closeButton.textContent = "Close";
      closeButton.addEventListener("click", async function (event) {
        event.stopPropagation();
        try {
          await fetchJSON("/api/sessions/" + encodeURIComponent(session.id) + "/close?csrf_token=" + encodeURIComponent(csrf), {
            method: "POST",
          }, true);
          if (session.id === state.activeSessionId) {
            disconnectSocket(false);
            state.activeSessionId = null;
          }
          await refreshSessions();
          if (!state.activeSessionId && state.sessions.length > 0) {
            activateSession(state.sessions[0].id);
          } else {
            updateUI();
          }
        } catch (error) {
          showBanner(error.message || "Could not close session.");
        }
      });

      actions.appendChild(closeButton);
      item.appendChild(meta);
      item.appendChild(actions);
      refs.sessionList.appendChild(item);
    });
  }

  function updateUI() {
    refs.connectionIndicator.dataset.state = state.connectionState;
    refs.connectionIndicator.textContent = connectionLabel(state.connectionState);
    refs.closeButton.disabled = !state.activeSessionId;

    if (!state.activeSessionId) {
      refs.ownerIndicator.textContent = "No session selected";
    } else if (state.connectionState === "ready") {
      refs.ownerIndicator.textContent = state.owner === "human" ? "You have control" : "Agent has control";
    } else if (state.connectionState === "session-lost") {
      refs.ownerIndicator.textContent = "Session unavailable";
    } else {
      refs.ownerIndicator.textContent = "Read-only until ready";
    }

    refs.takeControlButton.hidden = !(state.activeSessionId && state.connectionState === "ready" && state.owner === "agent");
    if (refs.takeControlButton.hidden) {
      refs.takeControlButton.disabled = false;
    }

    updateOverlay();
    renderSessionList();
    updateChatStatus();
  }

  function updateChatStatus() {
    refs.chatStatusIndicator.dataset.state = state.chatStatus;
    refs.chatStatusIndicator.textContent = state.chatStatus === "busy" ? "Busy" : "Idle";
  }

  function updateChatPanelState(entries) {
    const hasEntries = entries.length > 0;
    refs.chatPanel.classList.toggle("chat-panel--active", hasEntries);
    refs.chatPanel.classList.toggle("chat-panel--empty", !hasEntries);
  }

  function renderChatLog() {
    refs.chatLog.innerHTML = "";
    const entries = refs.chatLog.dataset.entries ? JSON.parse(refs.chatLog.dataset.entries) : [];
    updateChatPanelState(entries);
    if (entries.length === 0) {
      return;
    }

    entries.forEach(function (entry) {
      // 2026-08-21 UI design round: a small blue animated avatar next to
      // every agent reply, Kimi-style — "system"-kind entries are always
      // the human's own echoed message (see appendChatEntry's "You: "
      // call site), so that's the only kind that skips the avatar.
      const isAgent = entry.kind !== "system";
      const row = document.createElement("div");
      row.className = "chat-row" + (isAgent ? " chat-row--agent" : " chat-row--user");

      if (isAgent) {
        const avatar = document.createElement("div");
        avatar.className = "chat-avatar";
        avatar.setAttribute("aria-hidden", "true");
        row.appendChild(avatar);
      }

      const line = document.createElement("p");
      line.className = "chat-entry" + (entry.kind ? " " + entry.kind : "");
      line.textContent = entry.text;
      row.appendChild(line);

      refs.chatLog.appendChild(row);
    });

    if (state.chatThinking) {
      refs.chatLog.appendChild(renderThinkingRow());
    }

    refs.chatLog.scrollTop = refs.chatLog.scrollHeight;
  }

  // Same avatar as an agent reply, three breathing dots instead of text —
  // shown from submit until the first real "output" chunk (or the turn's
  // end) arrives. See state.chatThinking's doc comment for why this
  // matters more now than before chat_output_filter.go existed.
  function renderThinkingRow() {
    const row = document.createElement("div");
    row.className = "chat-row chat-row--agent";

    const avatar = document.createElement("div");
    avatar.className = "chat-avatar";
    avatar.setAttribute("aria-hidden", "true");
    row.appendChild(avatar);

    const bubble = document.createElement("p");
    bubble.className = "chat-entry chat-entry--thinking";
    bubble.setAttribute("aria-label", "The agent is thinking");
    for (let i = 0; i < 3; i++) {
      const dot = document.createElement("span");
      dot.className = "chat-thinking-dot";
      bubble.appendChild(dot);
    }
    row.appendChild(bubble);

    return row;
  }

  function appendChatEntry(text, kind) {
    if (!text) {
      return;
    }
    const entries = refs.chatLog.dataset.entries ? JSON.parse(refs.chatLog.dataset.entries) : [];
    entries.push({text: text, kind: kind || ""});
    refs.chatLog.dataset.entries = JSON.stringify(entries.slice(-200));
    renderChatLog();
  }

  function resetChatView() {
    refs.chatLog.dataset.entries = JSON.stringify([]);
    refs.chatInput.value = "";
    state.activeChatSessionClosed = false;
    state.sessionCloseBannerDismissed = false;
    hideSessionCloseBanner();
    updateChatComposerDisabled();
    refs.approvalBanner.hidden = true;
    refs.approvalSession.hidden = true;
    refs.approvalSession.textContent = "";
    refs.approvalPrompt.textContent = "";
    refs.approvalDetail.hidden = true;
    refs.approvalDetail.textContent = "";
    state.pendingApproval = null;
    state.chatStatus = "idle";
    state.chatThinking = false;
    updateChatStatus();
    setChatFeedback("");
    renderChatLog();
    refs.chatInput.focus();
  }

  function setChatFeedback(message) {
    if (!message) {
      refs.chatFeedback.hidden = true;
      refs.chatFeedback.textContent = "";
      return;
    }
    refs.chatFeedback.hidden = false;
    refs.chatFeedback.textContent = message;
  }

  function setChatSubmitting(submitting) {
    refs.chatInput.disabled = submitting;
    refs.chatSubmitButton.disabled = submitting;
  }

  function showApproval(event) {
    state.pendingApproval = event;
    refs.approvalPrompt.textContent = event.prompt || "Approval required.";
    if (event.session_id) {
      refs.approvalSession.hidden = false;
      refs.approvalSession.textContent = "For session " + event.session_id;
    } else {
      refs.approvalSession.hidden = true;
      refs.approvalSession.textContent = "";
    }
    if (event.detail) {
      refs.approvalDetail.hidden = false;
      refs.approvalDetail.textContent = event.detail;
    } else {
      refs.approvalDetail.hidden = true;
      refs.approvalDetail.textContent = "";
    }
    refs.approvalBanner.hidden = false;
  }

  async function respondToApproval(approved) {
    if (!state.pendingApproval) {
      return;
    }
    refs.approveButton.disabled = true;
    refs.denyButton.disabled = true;
    setChatFeedback("");
    try {
      await postJSON("/api/approve?csrf_token=" + encodeURIComponent(csrf), {
        approved: approved,
      });
      refs.approvalBanner.hidden = true;
      state.pendingApproval = null;
    } catch (error) {
      setChatFeedback(error.message || "Could not submit approval response.");
    } finally {
      refs.approveButton.disabled = false;
      refs.denyButton.disabled = false;
    }
  }

  function updateOverlay() {
    let title = "";
    let copy = "";

    if (!state.activeSessionId) {
      title = "No session selected";
      copy = "Create or select a session to begin.";
    } else if (state.connectionState === "connecting") {
      title = "Connecting";
      copy = "The terminal is read-only until the server sends the ready message.";
    } else if (state.connectionState === "reconnecting") {
      title = "Reconnecting";
      copy = "Trying to restore the terminal connection without buffering local input.";
    } else if (state.connectionState === "session-lost") {
      title = "Session lost";
      copy = "This session no longer exists on the server. Pick another session or create a new one.";
    } else if (state.connectionState === "disconnected") {
      title = "Disconnected";
      copy = "The terminal connection is closed.";
    } else if (state.connectionState === "ready" && state.owner === "agent") {
      title = "Agent has control";
      copy = "Typing and paste stay locked until you explicitly click Take control.";
    }

    if (!title) {
      refs.terminalOverlay.hidden = true;
      return;
    }

    refs.overlayTitle.textContent = title;
    refs.overlayCopy.textContent = copy;
    refs.terminalOverlay.hidden = false;
  }

  function resetTerminal(message) {
    terminal.reset();
    terminal.clear();
    if (message) {
      terminal.write(message.replace(/\n/g, "\r\n"));
    }
  }

  function setConnectionState(nextState) {
    state.connectionState = nextState;
  }

  function setCreateDisabled(disabled) {
    refs.workdirInput.disabled = disabled;
    refs.nameInput.disabled = disabled;
    document.getElementById("create-session-button").disabled = disabled;
  }

  function showBanner(message) {
    if (!message) {
      refs.banner.hidden = true;
      refs.banner.textContent = "";
      return;
    }
    refs.banner.hidden = false;
    refs.banner.textContent = message;
  }

  function metaContent(name) {
    const element = document.querySelector('meta[name="' + name + '"]');
    return element ? element.content : "";
  }

  function sessionLabel(session) {
    if (!session) {
      return "session";
    }
    return session.name || session.id;
  }

  function connectionLabel(value) {
    switch (value) {
      case "connecting":
        return "Connecting";
      case "ready":
        return "Ready";
      case "reconnecting":
        return "Reconnecting";
      case "session-lost":
        return "Session lost";
      case "disconnected":
      default:
        return "Disconnected";
    }
  }

  function wsURL(sessionID) {
    const protocol = window.location.protocol === "https:" ? "wss://" : "ws://";
    return protocol + window.location.host + "/api/terminal/" + encodeURIComponent(sessionID) + "/stream";
  }

  async function fetchJSON(url, options, allowEmpty) {
    const request = Object.assign({headers: {}}, options || {});
    request.headers = Object.assign({Accept: "application/json"}, request.headers || {});

    const response = await fetch(url, request);
    if (!response.ok) {
      const text = await response.text();
      throw new Error(text || ("Request failed with status " + response.status));
    }
    if (allowEmpty && response.status === 204) {
      return null;
    }
    const text = await response.text();
    return text ? JSON.parse(text) : null;
  }

  async function postJSON(url, body) {
    const response = await fetch(url, {
      method: "POST",
      headers: {
        Accept: "application/json",
        "Content-Type": "application/json",
      },
      body: JSON.stringify(body),
    });
    const text = await response.text();
    if (!response.ok) {
      throw new Error(errorMessageFromResponseText(text, response.status));
    }
    return text ? JSON.parse(text) : null;
  }

  // 2026-08-21: error bodies aren't always JSON — a security check that
  // rejects a request before it ever reaches a JSON-writing handler (e.g.
  // security.go's CSRF check) responds with plain text, and blindly
  // JSON.parse-ing that ("csrf token mismatch") threw its own confusing
  // "Unexpected token" SyntaxError instead of surfacing the real message.
  // Success responses in this app are always JSON (never called on the ok
  // path) — only error bodies need this tolerance.
  function errorMessageFromResponseText(text, status) {
    if (!text) {
      return "Request failed with status " + status;
    }
    try {
      const payload = JSON.parse(text);
      if (payload && payload.error) {
        return payload.error;
      }
    } catch (parseError) {
      // Not JSON — fall through and use the raw text below.
    }
    return text;
  }

  // patchJSON mirrors postJSON but for PATCH — the manual-edit write path's
  // method, since it's a partial update (a new atom) to an existing
  // object, not a create. Callers that need to distinguish a 409
  // stale_version conflict from any other failure should catch the thrown
  // Error and check error.message === "stale_version".
  async function patchJSON(url, body) {
    const response = await fetch(url, {
      method: "PATCH",
      headers: {
        Accept: "application/json",
        "Content-Type": "application/json",
      },
      body: JSON.stringify(body),
    });
    const text = await response.text();
    if (!response.ok) {
      throw new Error(errorMessageFromResponseText(text, response.status));
    }
    return text ? JSON.parse(text) : null;
  }

  // --- Canvas: sidebar (unchanged) / canvas center / chat right. Fetches
  // the current project's ProjectCanvas and renders materialized objects;
  // drafts are never shown here (they're not visible on the Canvas until
  // materialized — see canvasstore's Phase). Clicking a materialized
  // object opens the floating editing panel (openCanvasObjectPanel).

  async function refreshCanvasView() {
    if (!state.activeProjectPath) {
      state.canvas = null;
      renderCanvasView();
      return;
    }
    try {
      const pc = await fetchJSON("/api/canvases?project_path=" + encodeURIComponent(state.activeProjectPath));
      state.canvas = pc;
    } catch (error) {
      // Fail quiet — a broken Canvas fetch must not block the rest of the
      // UI, same posture as agenthost's dynamicCentro fail-open.
      state.canvas = null;
    }
    renderCanvasView();
  }

  function materializedCanvasObjects() {
    if (!state.canvas || !state.canvas.objects) {
      return [];
    }
    return state.canvas.objects.filter(function (obj) { return obj.phase === "materialized"; });
  }

  function currentAtomBody(objectId) {
    if (!state.canvas || !state.canvas.objects) {
      return null;
    }
    const obj = state.canvas.objects.find(function (o) { return o.object_id === objectId; });
    if (!obj || !obj.anchor_atom_ids || obj.anchor_atom_ids.length === 0) {
      return null;
    }
    const headId = obj.anchor_atom_ids[obj.anchor_atom_ids.length - 1];
    const atom = (state.canvas.atoms || []).find(function (a) { return a.atom_id === headId; });
    return atom ? atom.body : null;
  }

  // Three levels, per the 2026-08-20 UI design round: closed (just the
  // "diagramas" folder marker) -> grid (small file-style thumbnails, one
  // per materialized diagram) -> expanded (one diagram, full size). "Back"
  // always steps up exactly one level. None of this touches canvasstore
  // data — it's only what render() draws.
  function renderCanvasView() {
    refs.canvasObjects.innerHTML = "";
    refs.canvasEmptyState.hidden = true;

    const objects = materializedCanvasObjects();
    const expanded = state.expandedCanvasObjectId
      ? objects.find(function (o) { return o.object_id === state.expandedCanvasObjectId; })
      : null;
    // The expanded object may have been deactivated/deleted from under us
    // (e.g. edited in another tab) — fall back to the grid instead of
    // rendering a stale/missing diagram.
    if (state.expandedCanvasObjectId && !expanded) {
      state.expandedCanvasObjectId = null;
    }

    refs.canvasFolderMarker.hidden = state.canvasFolderOpen;
    refs.canvasFolderBackButton.hidden = !state.canvasFolderOpen;
    refs.canvasDiagramArea.classList.toggle("canvas-diagram-area--open", state.canvasFolderOpen);
    refs.canvasObjects.classList.toggle("canvas-objects--grid", state.canvasFolderOpen && !expanded);
    refs.canvasObjects.classList.toggle("canvas-objects--expanded", state.canvasFolderOpen && !!expanded);

    if (!state.canvasFolderOpen) {
      return;
    }

    if (expanded) {
      refs.canvasObjects.appendChild(renderExpandedDiagram(expanded));
      return;
    }

    objects.forEach(function (obj) {
      refs.canvasObjects.appendChild(renderDiagramThumbnail(obj));
    });
  }

  // A small "file" thumbnail: fixed-size preview box (the diagram scaled
  // down to fit) with the object's name below it, grid-arranged like
  // Finder icon view. Click expands it — editing still happens one level
  // deeper, from the expanded view.
  function renderDiagramThumbnail(obj) {
    const file = document.createElement("div");
    file.className = "canvas-diagram-file";
    file.tabIndex = 0;
    file.setAttribute("role", "button");

    const thumb = document.createElement("div");
    thumb.className = "canvas-diagram-thumb";
    if (obj.type === "diagram") {
      const body = currentAtomBody(obj.object_id) || obj.payload;
      const stage = renderDiagramStage(body);
      // The stage carries its own explicit px width/height (set in
      // renderDiagramStage from the diagram's layout) — scale it down to
      // fit inside the fixed thumbnail box instead of cropping it, same
      // "contain" idea as a file manager's image thumbnails. Never
      // upscale a diagram that's already smaller than the box.
      const stageW = parseFloat(stage.style.width) || 300;
      const stageH = parseFloat(stage.style.height) || 160;
      const scale = Math.min(108 / stageW, 72 / stageH, 1);
      stage.style.transform = "scale(" + scale + ")";
      thumb.appendChild(stage);
    }
    file.appendChild(thumb);

    const name = document.createElement("span");
    name.className = "canvas-diagram-file__name";
    name.textContent = obj.name;
    file.appendChild(name);

    const expand = function () {
      state.expandedCanvasObjectId = obj.object_id;
      renderCanvasView();
    };
    file.addEventListener("click", expand);
    file.addEventListener("keydown", function (event) {
      if (event.key === "Enter" || event.key === " ") {
        event.preventDefault();
        expand();
      }
    });

    return file;
  }

  // Full-size diagram, header with name/active badge + an explicit Edit
  // button (the manual/mini-chat panel from before — unchanged).
  function renderExpandedDiagram(obj) {
    const wrap = document.createElement("div");
    wrap.className = "canvas-diagram-expanded";

    const header = document.createElement("div");
    header.className = "canvas-object-card__header";
    const name = document.createElement("span");
    name.className = "canvas-object-card__name";
    name.textContent = obj.name;
    const badge = document.createElement("span");
    badge.className = "canvas-object-card__badge";
    badge.dataset.active = obj.activation === "active" ? "true" : "false";
    badge.textContent = obj.activation === "active" ? "Active" : obj.type;
    const editButton = document.createElement("button");
    editButton.type = "button";
    editButton.className = "ghost-button";
    editButton.textContent = "Edit";
    editButton.addEventListener("click", function () { openCanvasObjectPanel(obj.object_id); });
    header.appendChild(name);
    header.appendChild(badge);
    header.appendChild(editButton);
    wrap.appendChild(header);

    if (obj.type === "diagram") {
      const body = currentAtomBody(obj.object_id) || obj.payload;
      const stageWrap = document.createElement("div");
      stageWrap.className = "canvas-diagram-expanded__stage";
      stageWrap.appendChild(renderDiagramStage(body));
      wrap.appendChild(stageWrap);
    }

    return wrap;
  }

  // autoLayoutNodes assigns a simple left-to-right, wrapping grid position
  // to any node missing explicit x/y. The model's payload today only ever
  // emits id/label/type per node — canvas_create_draft/canvas_edit_object's
  // schemas don't ask for layout coordinates, and asking an LLM for
  // precise, non-overlapping pixel positions for an arbitrary graph is a
  // known-fragile thing to rely on — so every node used to default to the
  // same (0,0) box and stack exactly on top of each other. A node that
  // *does* specify x/y is left untouched: this is only ever a fallback,
  // never an override, so a human's (or a future model's) deliberate
  // layout still wins.
  function autoLayoutNodes(nodes) {
    const defaultW = 160, defaultH = 60, colGap = 40, rowGap = 30, cols = 3, margin = 20;
    let col = 0, row = 0;
    return nodes.map(function (node) {
      const hasPosition = typeof node.x === "number" && typeof node.y === "number";
      if (hasPosition) {
        return node;
      }
      const positioned = Object.assign({}, node, {
        x: margin + col * (defaultW + colGap),
        y: margin + row * (defaultH + rowGap),
        w: node.w || defaultW,
        h: node.h || defaultH,
      });
      col += 1;
      if (col >= cols) {
        col = 0;
        row += 1;
      }
      return positioned;
    });
  }

  // renderDiagramStage renders a diagram payload (§6 of the build plan:
  // nodes/edges/groups/layout/style_tokens) as absolutely-positioned node
  // divs over an SVG edge overlay, inside a stage sized from
  // layout.width/height, or derived from the laid-out nodes when the
  // payload doesn't specify one. style tokens map 1:1 to fixed CSS
  // classes — never arbitrary inline colors.
  function renderDiagramStage(payload) {
    const stage = document.createElement("div");
    stage.className = "canvas-diagram-stage";
    if (!payload) {
      stage.style.height = "80px";
      return stage;
    }
    const nodes = autoLayoutNodes(payload.nodes || []);
    const explicitLayout = payload.layout || {};
    let width = explicitLayout.width;
    let height = explicitLayout.height;
    if (!width || !height) {
      let maxX = 0, maxY = 0;
      nodes.forEach(function (node) {
        maxX = Math.max(maxX, (node.x || 0) + (node.w || 160));
        maxY = Math.max(maxY, (node.y || 0) + (node.h || 60));
      });
      width = width || Math.max(maxX + 20, 300);
      height = height || Math.max(maxY + 20, 160);
    }
    stage.style.width = width + "px";
    stage.style.height = height + "px";

    (payload.groups || []).forEach(function (group) {
      const frame = document.createElement("div");
      frame.className = "canvas-group-frame";
      frame.style.left = (group.x || 0) + "px";
      frame.style.top = (group.y || 0) + "px";
      frame.style.width = (group.w || 100) + "px";
      frame.style.height = (group.h || 100) + "px";
      stage.appendChild(frame);
      if (group.label) {
        const label = document.createElement("span");
        label.className = "canvas-group-label";
        label.style.left = (group.x || 0) + 4 + "px";
        label.style.top = (group.y || 0) - 16 + "px";
        label.textContent = group.label;
        stage.appendChild(label);
      }
    });

    const svgNS = "http://www.w3.org/2000/svg";
    const svg = document.createElementNS(svgNS, "svg");
    svg.setAttribute("class", "canvas-diagram-svg");
    svg.setAttribute("width", String(width));
    svg.setAttribute("height", String(height));

    const nodeById = {};
    nodes.forEach(function (node) { nodeById[node.id] = node; });

    (payload.edges || []).forEach(function (edge) {
      const from = nodeById[edge.from];
      const to = nodeById[edge.to];
      if (!from || !to) {
        return;
      }
      const line = document.createElementNS(svgNS, "line");
      line.setAttribute("x1", String((from.x || 0) + (from.w || 160) / 2));
      line.setAttribute("y1", String((from.y || 0) + (from.h || 60) / 2));
      line.setAttribute("x2", String((to.x || 0) + (to.w || 160) / 2));
      line.setAttribute("y2", String((to.y || 0) + (to.h || 60) / 2));
      line.setAttribute("class", "canvas-edge-line" + (edge.style ? " canvas-edge-line--" + edge.style : ""));
      svg.appendChild(line);
    });
    stage.appendChild(svg);

    nodes.forEach(function (node) {
      const el = document.createElement("div");
      el.className = "canvas-node" + (node.style ? " canvas-node--" + node.style : "");
      el.style.left = (node.x || 0) + "px";
      el.style.top = (node.y || 0) + "px";
      el.style.width = (node.w || 160) + "px";
      el.style.height = (node.h || 60) + "px";
      el.textContent = node.label || "";
      stage.appendChild(el);
    });

    return stage;
  }

  function openCanvasObjectPanel(objectId) {
    if (!state.canvas) {
      return;
    }
    const obj = (state.canvas.objects || []).find(function (o) { return o.object_id === objectId; });
    if (!obj) {
      return;
    }
    state.activeCanvasObjectId = objectId;
    refs.canvasObjectPanelTitle.textContent = obj.name;
    const body = currentAtomBody(objectId) || obj.payload || {};
    refs.canvasObjectPayloadInput.value = JSON.stringify(body, null, 2);
    refs.canvasObjectSaveFeedback.textContent = "";
    refs.canvasMinichatLog.innerHTML = "";
    renderCanvasObjectActivationToggle();
    refs.canvasObjectPanel.hidden = false;
    refs.canvasObjectPayloadInput.focus();
  }

  // Mirrors the card badge's active/inactive look (canvas-object-card__badge
  // in renderCanvasView), but this one is the real control — see the click
  // handler below.
  function renderCanvasObjectActivationToggle() {
    if (!state.canvas || !state.activeCanvasObjectId) {
      return;
    }
    const obj = (state.canvas.objects || []).find(function (o) { return o.object_id === state.activeCanvasObjectId; });
    if (!obj) {
      return;
    }
    const active = obj.activation === "active";
    refs.canvasObjectActivationToggle.dataset.active = active ? "true" : "false";
    refs.canvasObjectActivationToggle.textContent = active ? "Active — click to un-anchor" : "Anchor to chat";
  }

  // setCanvasObjectActivation calls the same POST /activate|/deactivate
  // endpoint the QA findings doc identified as already built but never
  // wired up (termserver/canvas.go's handleCanvasObjectActivation, backed
  // by canvasstore.SetActivation) — no new request helper, reuses postJSON.
  function setCanvasObjectActivation(objectId, action) {
    const url = "/api/canvases/objects/" + encodeURIComponent(objectId) + "/" + action +
      "?project_path=" + encodeURIComponent(state.activeProjectPath) +
      "&csrf_token=" + encodeURIComponent(csrf);
    return postJSON(url, {});
  }

  // Activation toggle: same 409/stale_version convention as the manual-edit
  // PATCH above, except here the retry is automatic and single-shot rather
  // than asking the human to click Save again — a toggle click carries a
  // fixed, unambiguous intent (flip to the opposite of what was last
  // known), so replaying that same action once against a freshly-reloaded
  // canvas is safe where blindly replaying an arbitrary payload edit would
  // not be.
  refs.canvasObjectActivationToggle.addEventListener("click", async function () {
    if (!state.activeCanvasObjectId || !state.activeProjectPath || !state.canvas) {
      return;
    }
    const obj = (state.canvas.objects || []).find(function (o) { return o.object_id === state.activeCanvasObjectId; });
    if (!obj) {
      return;
    }
    const action = obj.activation === "active" ? "deactivate" : "activate";
    refs.canvasObjectActivationToggle.disabled = true;
    try {
      state.canvas = await setCanvasObjectActivation(state.activeCanvasObjectId, action);
    } catch (error) {
      if (error && error.message === "stale_version") {
        await refreshCanvasView();
        try {
          state.canvas = await setCanvasObjectActivation(state.activeCanvasObjectId, action);
        } catch (retryError) {
          refs.canvasObjectSaveFeedback.textContent = retryError.message || "Could not update activation.";
        }
      } else {
        refs.canvasObjectSaveFeedback.textContent = error.message || "Could not update activation.";
      }
    } finally {
      refs.canvasObjectActivationToggle.disabled = false;
      renderCanvasObjectActivationToggle();
      renderCanvasView();
    }
  });

  function closeCanvasObjectPanel() {
    refs.canvasObjectPanel.hidden = true;
    state.activeCanvasObjectId = null;
  }

  refs.canvasObjectPanelClose.addEventListener("click", closeCanvasObjectPanel);
  refs.canvasObjectPanelBackdrop.addEventListener("click", closeCanvasObjectPanel);
  document.addEventListener("keydown", function (event) {
    if (event.key === "Escape" && !refs.canvasObjectPanel.hidden) {
      closeCanvasObjectPanel();
    }
  });

  // Manual edit save: PATCH with expected_version = the canvas version this
  // panel was opened against. A 409 stale_version means someone else (a
  // tool-driven edit, or another tab) wrote in between — per the build
  // spec's CAS guarantee, refetch and let the user retry rather than
  // silently overwriting.
  refs.canvasObjectSaveButton.addEventListener("click", async function () {
    if (!state.activeCanvasObjectId || !state.activeProjectPath || !state.canvas) {
      return;
    }
    let payload;
    try {
      payload = JSON.parse(refs.canvasObjectPayloadInput.value);
    } catch (error) {
      refs.canvasObjectSaveFeedback.textContent = "Invalid JSON: " + error.message;
      return;
    }
    refs.canvasObjectSaveFeedback.textContent = "Saving…";
    try {
      const url = "/api/canvases/objects/" + encodeURIComponent(state.activeCanvasObjectId) +
        "?project_path=" + encodeURIComponent(state.activeProjectPath) +
        "&csrf_token=" + encodeURIComponent(csrf);
      const updated = await patchJSON(url, {payload: payload, expected_version: state.canvas.version});
      state.canvas = updated;
      refs.canvasObjectSaveFeedback.textContent = "Saved.";
      renderCanvasView();
    } catch (error) {
      if (error && error.message === "stale_version") {
        refs.canvasObjectSaveFeedback.textContent = "Someone else edited this object — reloading, please retry.";
        await refreshCanvasView();
      } else {
        refs.canvasObjectSaveFeedback.textContent = error.message || "Save failed.";
      }
    }
  });

  // The mini-chat sends an ordinary chat message prefixed with which Canvas
  // object it's about, through the same /api/chat path as the main
  // composer, so the agent's normal edit/read tools (and the dynamicCentro
  // anchor, if this object happens to be active) apply exactly as they
  // would from the main chat. The text prefix alone used to be the *only*
  // scoping — advisory, not enforced — which let the model act on a
  // different Canvas object than the one this panel is for whenever more
  // than one object was anchored at once (confirmed in live testing: an
  // edit meant for one diagram landed on another, and a duplicate object
  // got created). canvas_object_id below is the real, enforced scope: the
  // server rejects any canvas_edit_object/activate/deactivate/create_draft/
  // materialize_draft call this turn that doesn't target this exact object
  // (agenthost's canvasCell.checkScope) — the browser states which object's
  // panel is open, the model never gets to guess.
  refs.canvasMinichatForm.addEventListener("submit", async function (event) {
    event.preventDefault();
    const message = refs.canvasMinichatInput.value.trim();
    if (!message || !state.activeCanvasObjectId || !state.canvas) {
      return;
    }
    const scopedObjectId = state.activeCanvasObjectId;
    const obj = (state.canvas.objects || []).find(function (o) { return o.object_id === scopedObjectId; });
    const scoped = "Regarding Canvas object \"" + (obj ? obj.name : "") + "\" (object_id " +
      scopedObjectId + "): " + message;
    const entry = document.createElement("p");
    entry.textContent = "You: " + message;
    refs.canvasMinichatLog.appendChild(entry);
    refs.canvasMinichatInput.value = "";
    try {
      const body = {message: scoped};
      if (state.activeChatSessionId) {
        body.session_id = state.activeChatSessionId;
      }
      if (state.activeProjectPath) {
        body.project_path = state.activeProjectPath;
      }
      body.planning_id = "";
      body.board_id = "";
      body.client_id = clientId;
      body.turn_id = newTurnId();
      body.canvas_object_id = scopedObjectId;
      await postJSON("/api/chat?csrf_token=" + encodeURIComponent(csrf), body);
    } catch (error) {
      const errEntry = document.createElement("p");
      errEntry.textContent = "Error: " + (error.message || "could not send");
      refs.canvasMinichatLog.appendChild(errEntry);
    }
  });

  // --- Materialize signal (canvas_suggest SSE event) ---
  //
  // The button is a contextual affordance, never the only way to
  // materialize — natural language ("materialízalo") and the /materialize
  // slash command both work independent of whether this banner ever
  // appears (see canvas_tools.go / termserver/canvas.go).

  function showCanvasSuggestBanner(suggestion) {
    state.pendingCanvasSuggestion = suggestion;
    refs.canvasSuggestText.textContent = "\"" + suggestion.name + "\" looks ready to materialize.";
    refs.canvasSuggestBanner.hidden = false;
  }

  function hideCanvasSuggestBanner() {
    state.pendingCanvasSuggestion = null;
    refs.canvasSuggestBanner.hidden = true;
  }

  refs.canvasFolderMarker.addEventListener("click", function () {
    state.canvasFolderOpen = true;
    state.expandedCanvasObjectId = null;
    renderCanvasView();
  });

  refs.canvasFolderBackButton.addEventListener("click", function () {
    if (state.expandedCanvasObjectId) {
      state.expandedCanvasObjectId = null;
    } else {
      state.canvasFolderOpen = false;
    }
    renderCanvasView();
  });

  // Lower panel's "second chat" (2026-08-21 UI design round) — UI-only by
  // deliberate choice: local echo of whatever's typed, nothing sent to
  // /api/chat, no session, no agent reply. What this chat is actually for
  // (multi-AI? something else?) is still undecided — this just gives the
  // design something concrete to react to. renderLowerChat/lowerChatEntries
  // mirror renderChatLog/appendChatEntry's shape on purpose so upgrading
  // this to something real later is a small diff, not a rewrite.
  let lowerChatEntries = [];

  function renderLowerChat() {
    refs.lowerChatLog.innerHTML = "";
    if (lowerChatEntries.length === 0) {
      const empty = document.createElement("p");
      empty.className = "lower-chat-empty muted";
      empty.textContent = "Nothing here yet — this is a second chat, still just a mockup.";
      refs.lowerChatLog.appendChild(empty);
      return;
    }
    lowerChatEntries.forEach(function (entry) {
      const row = document.createElement("div");
      row.className = "chat-row" + (entry.isAgent ? " chat-row--agent" : " chat-row--user");
      if (entry.isAgent) {
        const avatar = document.createElement("div");
        avatar.className = "chat-avatar";
        avatar.setAttribute("aria-hidden", "true");
        row.appendChild(avatar);
      }
      const bubble = document.createElement("p");
      bubble.className = "chat-entry" + (entry.isAgent ? "" : " system");
      bubble.textContent = entry.text;
      row.appendChild(bubble);
      refs.lowerChatLog.appendChild(row);
    });
    refs.lowerChatLog.scrollTop = refs.lowerChatLog.scrollHeight;
  }

  refs.lowerChatForm.addEventListener("submit", function (event) {
    event.preventDefault();
    const message = refs.lowerChatInput.value.trim();
    if (!message) {
      return;
    }
    lowerChatEntries.push({text: message, isAgent: false});
    refs.lowerChatInput.value = "";
    renderLowerChat();
  });

  refs.canvasSuggestDismiss.addEventListener("click", hideCanvasSuggestBanner);

  refs.canvasSuggestButton.addEventListener("click", async function () {
    if (!state.pendingCanvasSuggestion) {
      return;
    }
    const name = state.pendingCanvasSuggestion.name;
    hideCanvasSuggestBanner();
    try {
      const body = {message: "/materialize " + name};
      if (state.activeChatSessionId) {
        body.session_id = state.activeChatSessionId;
      }
      if (state.activeProjectPath) {
        body.project_path = state.activeProjectPath;
      }
      body.planning_id = "";
      body.board_id = "";
      body.client_id = clientId;
      body.turn_id = newTurnId();
      await postJSON("/api/chat?csrf_token=" + encodeURIComponent(csrf), body);
      await refreshCanvasView();
    } catch (error) {
      setChatFeedback(error.message || "Could not materialize.");
    }
  });
})();
