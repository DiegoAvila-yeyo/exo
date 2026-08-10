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
  };

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
  };

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
      await postJSON("/api/chat?csrf_token=" + encodeURIComponent(csrf), {
        message: message,
      });
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
    resetChatView();
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
      case "done":
        state.chatStatus = "idle";
        updateChatStatus();
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
