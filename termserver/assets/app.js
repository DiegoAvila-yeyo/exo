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
    pendingApproval: null,
    chatSessions: [],
    activeChatSessionId: null,
    // The workspace's current project — independent of whichever chat is
    // open. Set from the sidebar, sent on every message, and used to filter
    // "Recent" down to that project's sessions.
    activeProjectPath: null,
    activeProjectName: null,
    // --- Planning (Round 1) ---
    view: "home", // "home" | "planning-list" | "planning-board"
    plannings: [],
    activePlanning: null, // full Planning object (boards + knowledge) once opened
    activeBoardId: null,
    // Round 3: turn_id of this tab's most recent submit — a `navigate`
    // event carrying any other turn_id is stale (e.g. arrived late after a
    // reconnect, after the user already moved on) and must be ignored.
    lastTurnId: null,
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
      appendChatEntry("You: " + message, "system");
      refs.chatInput.value = "";
    } catch (error) {
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
    } catch (error) {
      setChatFeedback(error.message || "Could not open that chat.");
    }
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

  async function loadProjectList() {
    try {
      const list = await fetchJSON("/api/projects", {method: "GET"});
      renderProjectList(Array.isArray(list) ? list : []);
    } catch (error) {
      renderProjectList([]);
    }
  }

  function renderProjectList(list) {
    refs.projectList.innerHTML = "";
    if (list.length === 0) {
      const empty = document.createElement("p");
      empty.className = "sidebar-placeholder";
      empty.textContent = "No projects found.";
      refs.projectList.appendChild(empty);
      return;
    }
    list.forEach(function (project) {
      const item = document.createElement("button");
      item.type = "button";
      item.className = "chat-session-item" + (project.path === state.activeProjectPath ? " active" : "");
      item.textContent = project.name;
      item.title = project.path;
      item.addEventListener("click", function () {
        setActiveProject(project.name, project.path);
        renderProjectList(list); // re-mark which row is active
      });
      refs.projectList.appendChild(item);
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
        updateChatStatus();
        break;
      case "busy":
        state.chatStatus = "busy";
        updateChatStatus();
        break;
      case "output":
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
        break;
      default:
        break;
    }
  }

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
      const line = document.createElement("p");
      line.className = "chat-entry" + (entry.kind ? " " + entry.kind : "");
      line.textContent = entry.text;
      refs.chatLog.appendChild(line);
    });
    refs.chatLog.scrollTop = refs.chatLog.scrollHeight;
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
    refs.approvalBanner.hidden = true;
    refs.approvalSession.hidden = true;
    refs.approvalSession.textContent = "";
    refs.approvalPrompt.textContent = "";
    refs.approvalDetail.hidden = true;
    refs.approvalDetail.textContent = "";
    state.pendingApproval = null;
    state.chatStatus = "idle";
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
    const payload = text ? JSON.parse(text) : null;
    if (!response.ok) {
      if (payload && payload.error) {
        throw new Error(payload.error);
      }
      throw new Error(text || ("Request failed with status " + response.status));
    }
    return payload;
  }
})();
