const api = () => window.go?.gui?.Bridge;

const state = {
  app: null,
  selectedChannel: "",
  draft: {
    subject: "",
    body: "",
    replyTo: "",
    experimentID: "",
    experimentSHA: "",
  },
  modal: null,
  status: null,
};

const el = (tag, className, text) => {
  const node = document.createElement(tag);
  if (className) node.className = className;
  if (text !== undefined) node.textContent = text;
  return node;
};

const escapeHTML = (value) =>
  String(value ?? "")
    .replaceAll("&", "&amp;")
    .replaceAll("<", "&lt;")
    .replaceAll(">", "&gt;")
    .replaceAll('"', "&quot;");

const showStatus = (kind, text) => {
  state.status = { kind, text };
  render();
  window.clearTimeout(showStatus.timer);
  showStatus.timer = window.setTimeout(() => {
    state.status = null;
    render();
  }, 3200);
};

const call = async (method, payload) => {
  const bridge = api();
  if (!bridge || typeof bridge[method] !== "function") {
    throw new Error("Wails bridge is not available yet");
  }
  return bridge[method](payload);
};

const refreshState = async (selectedChannel = state.selectedChannel) => {
  const appState = await call("GetState", selectedChannel || "");
  state.app = appState;
  state.selectedChannel = appState.selectedChannel || "";
  if (!state.draft.experimentID && appState.experiments.length > 0) {
    state.draft.experimentID = appState.experiments[0].id;
  }
  render();
};

const submitMessage = async () => {
  try {
    const appState = await call("SendMessage", {
      userID: state.app?.currentUser || "",
      channelID: state.selectedChannel,
      subject: state.draft.subject,
      body: state.draft.body,
      replyTo: state.draft.replyTo,
      experimentID: state.draft.experimentID,
      experimentSHA: state.draft.experimentSHA,
    });
    state.app = appState;
    state.selectedChannel = appState.selectedChannel || "";
    state.draft.subject = "";
    state.draft.body = "";
    state.draft.replyTo = "";
    state.draft.experimentSHA = "";
    render();
    showStatus("success", "Message sent");
  } catch (err) {
    showStatus("error", err.message || String(err));
  }
};

const modalPresets = {
  createUser: {
    title: "Create user",
    description: "Create the writer branch for a user and make it the default sender.",
    fields: [
      { name: "userID", label: "User ID", placeholder: "alice", value: () => state.app?.currentUser || "" },
    ],
    action: async (values) => {
      const appState = await call("CreateUser", values);
      state.app = appState;
      render();
      showStatus("success", `User ${values.userID} created`);
    },
  },
  createChannel: {
    title: "Create channel",
    description: "Open a new channel branch on top of main.",
    fields: [
      { name: "channelID", label: "Channel ID", placeholder: "research" },
      { name: "title", label: "Title", placeholder: "Research" },
      { name: "creator", label: "Creator", placeholder: "alice", value: () => state.app?.currentUser || "" },
    ],
    action: async (values) => {
      const appState = await call("CreateChannel", values);
      state.app = appState;
      state.selectedChannel = appState.selectedChannel || values.channelID;
      render();
      showStatus("success", `Channel ${values.channelID} created`);
    },
  },
  addMember: {
    title: "Add member",
    description: "Record a membership event on the selected channel branch.",
    fields: [
      { name: "channelID", label: "Channel ID", placeholder: "research", value: () => state.selectedChannel },
      { name: "member", label: "Member", placeholder: "bob" },
      { name: "actor", label: "Actor", placeholder: "alice", value: () => state.app?.currentUser || "" },
    ],
    action: async (values) => {
      const appState = await call("AddChannelMember", values);
      state.app = appState;
      render();
      showStatus("success", `Added ${values.member} to ${values.channelID}`);
    },
  },
  createExperiment: {
    title: "Create experiment",
    description: "Start an experiment branch and register its config.",
    fields: [
      { name: "experimentID", label: "Experiment ID", placeholder: "exp-search-ranking" },
      { name: "title", label: "Title", placeholder: "Search ranking trial" },
      { name: "baseRef", label: "Base ref", placeholder: "HEAD", value: () => "HEAD" },
      { name: "actor", label: "Actor", placeholder: "alice", value: () => state.app?.currentUser || "" },
      { name: "configJSON", label: "Config JSON", placeholder: "{\"model\":\"gpt-5.4\"}", multiline: true, value: () => "{}" },
    ],
    action: async (values) => {
      const appState = await call("CreateExperiment", values);
      state.app = appState;
      render();
      showStatus("success", `Experiment ${values.experimentID} created`);
    },
  },
  retainExperiment: {
    title: "Retain attempt",
    description: "Merge an attempt SHA into an experiment branch using the retain flow.",
    fields: [
      { name: "experimentID", label: "Experiment ID", placeholder: "exp-search-ranking", value: () => state.app?.experiments?.[0]?.id || "" },
      { name: "ref", label: "Attempt SHA / ref", placeholder: "abc123def456" },
    ],
    action: async (values) => {
      const appState = await call("RetainExperiment", values);
      state.app = appState;
      render();
      showStatus("success", `Retained ${values.ref}`);
    },
  },
};

const openModal = (kind) => {
  const preset = modalPresets[kind];
  state.modal = {
    kind,
    values: Object.fromEntries(
      preset.fields.map((field) => [field.name, field.value ? field.value() : ""]),
    ),
  };
  render();
};

const closeModal = () => {
  state.modal = null;
  render();
};

const submitModal = async () => {
  if (!state.modal) return;
  const preset = modalPresets[state.modal.kind];
  try {
    await preset.action(state.modal.values);
    state.modal = null;
    render();
  } catch (err) {
    showStatus("error", err.message || String(err));
  }
};

const renderMessages = () => {
  if (!state.app?.messages?.length) {
    return `
      <div class="empty-state">
        No messages yet. Start the thread in <strong>#${escapeHTML(state.selectedChannel || "channel")}</strong>.
      </div>
    `;
  }
  return state.app.messages
    .map((message) => {
      const mine = message.userID === state.app.currentUser ? " mine" : "";
      const reply = message.replyTo ? `<span class="message-thread">reply to ${escapeHTML(message.replyTo.slice(0, 10))}</span>` : "";
      const tags = [];
      if (message.experimentID) {
        const sha = message.experimentSHA ? ` · ${escapeHTML(message.experimentSHA)}` : "";
        tags.push(`<span class="tag experiment">experiment ${escapeHTML(message.experimentID)}${sha}</span>`);
      }
      return `
        <article class="message-card${mine}">
          <div class="message-top">
            <span class="message-author">${escapeHTML(message.userID)}</span>
            <span class="message-time">${escapeHTML(message.createdAt)}</span>
            <span class="message-sha">${escapeHTML(message.shortHash)}</span>
            ${reply}
          </div>
          <div class="message-subject">${escapeHTML(message.subject || "(no subject)")}</div>
          <div class="message-body">${escapeHTML(message.body || "")}</div>
          ${tags.length ? `<div class="message-tags">${tags.join("")}</div>` : ""}
        </article>
      `;
    })
    .join("");
};

const renderModal = () => {
  if (!state.modal) return "";
  const preset = modalPresets[state.modal.kind];
  const fields = preset.fields
    .map((field) => {
      const value = state.modal.values[field.name] || "";
      if (field.multiline) {
        return `
          <div class="field">
            <label>${escapeHTML(field.label)}</label>
            <textarea data-modal-field="${escapeHTML(field.name)}" placeholder="${escapeHTML(field.placeholder || "")}">${escapeHTML(value)}</textarea>
          </div>
        `;
      }
      return `
        <div class="field">
          <label>${escapeHTML(field.label)}</label>
          <input data-modal-field="${escapeHTML(field.name)}" value="${escapeHTML(value)}" placeholder="${escapeHTML(field.placeholder || "")}" />
        </div>
      `;
    })
    .join("");
  return `
    <div class="modal-backdrop" data-close-modal="true">
      <div class="modal" onclick="event.stopPropagation()">
        <div class="modal-header">
          <h2>${escapeHTML(preset.title)}</h2>
          <p>${escapeHTML(preset.description)}</p>
        </div>
        <div class="modal-body">${fields}</div>
        <div class="modal-actions">
          <button type="button" data-close-modal="true">Cancel</button>
          <button type="button" class="primary" data-submit-modal="true">Save</button>
        </div>
      </div>
    </div>
  `;
};

const render = () => {
  const root = document.querySelector("#app");
  if (!state.app) {
    root.innerHTML = `
      <div class="shell">
        <main class="main-panel">
          <div class="empty-state">Loading GitChat desktop…</div>
        </main>
      </div>
    `;
    return;
  }

  const channels = state.app.channels
    .map(
      (channel) => `
        <button class="channel-item ${channel.id === state.selectedChannel ? "active" : ""}" data-channel-id="${escapeHTML(channel.id)}">
          <span class="channel-name"># ${escapeHTML(channel.id)}</span>
          <span class="channel-meta">${escapeHTML(channel.title)} · ${escapeHTML(channel.creator || "unknown")}</span>
        </button>
      `,
    )
    .join("");

  const experiments = state.app.experiments
    .map(
      (experiment) => `
        <div class="experiment-item">
          <span class="experiment-name">${escapeHTML(experiment.id)}</span>
          <span class="experiment-meta">${escapeHTML(experiment.title)} · ${escapeHTML(experiment.creator || "unknown")}</span>
        </div>
      `,
    )
    .join("");

  const selectedTitle = state.app.selectedChannelTitle || "Pick a channel";
  const selectedMeta = state.selectedChannel
    ? `Channel branch: channels/${escapeHTML(state.selectedChannel)}`
    : "Create a channel to start chatting";

  root.innerHTML = `
    <div class="shell">
      <aside class="workspace-rail">
        <div class="workspace-badge">GC</div>
        <div class="workspace-user">
          <div>GitChat</div>
          <div>${escapeHTML(state.app.currentUser || "unconfigured user")}</div>
        </div>
      </aside>

      <aside class="sidebar">
        <div class="sidebar-header">
          <h1 class="sidebar-title">GitChat</h1>
          <p class="sidebar-subtitle">Branches, messages, and experiments in one desktop view.</p>
        </div>
        <div class="sidebar-actions">
          <button class="primary" data-open-modal="createChannel">New channel</button>
          <button data-open-modal="createUser">Create user</button>
        </div>
        <section class="sidebar-section">
          <div class="section-label">Channels</div>
          <div class="channel-list">${channels || '<div class="empty-state">No channels yet.</div>'}</div>
        </section>
        <section class="sidebar-section">
          <div class="section-label">Experiments</div>
          <div class="experiment-list">${experiments || '<div class="empty-state">No experiments indexed.</div>'}</div>
        </section>
      </aside>

      <main class="main-panel">
        <header class="panel-header">
          <div>
            <h2 class="panel-title"># ${escapeHTML(selectedTitle)}</h2>
            <p class="panel-subtitle">${selectedMeta}</p>
          </div>
          <div class="toolbar">
            <button class="primary" data-refresh="true">Refresh</button>
            <button data-open-modal="addMember">Add member</button>
            <button data-open-modal="createExperiment">New experiment</button>
            <button data-open-modal="retainExperiment">Retain attempt</button>
          </div>
        </header>
        <section class="messages">${renderMessages()}</section>
        <section class="composer">
          <div class="composer-card">
            <div class="composer-meta">
              <div class="field">
                <label>Subject</label>
                <input id="subject" placeholder="Short summary" value="${escapeHTML(state.draft.subject)}" />
              </div>
              <div class="field">
                <label>Reply To</label>
                <input id="replyTo" placeholder="Optional commit hash" value="${escapeHTML(state.draft.replyTo)}" />
              </div>
              <div class="field">
                <label>Experiment</label>
                <input id="experimentID" placeholder="Optional experiment id" value="${escapeHTML(state.draft.experimentID)}" />
              </div>
            </div>
            <div class="field" style="padding: 14px 14px 0;">
              <label>Message</label>
              <textarea id="body" placeholder="Write a channel update, design note, or experiment handoff.">${escapeHTML(state.draft.body)}</textarea>
            </div>
            <div class="composer-actions">
              <div class="hint">Slack-like shell, backed by your existing GitChat branch model.</div>
              <button class="primary" data-send="true">Send message</button>
            </div>
          </div>
        </section>
      </main>

      <aside class="detail-panel">
        <div class="detail-header">
          <h2 class="detail-title">Context</h2>
          <p class="detail-copy">Keep operational actions close to the conversation, and keep experiments visible without leaving the thread.</p>
        </div>
        <div class="detail-actions">
          <button class="primary" data-open-modal="createExperiment">Create experiment</button>
          <button data-open-modal="retainExperiment">Retain SHA</button>
        </div>
        <section class="detail-section">
          <h3>Current user</h3>
          <div class="detail-card">
            <strong>${escapeHTML(state.app.currentUser || "No default user")}</strong>
            <span>Messages are sent from the matching users/&lt;id&gt; branch.</span>
          </div>
        </section>
        <section class="detail-section">
          <h3>Recent experiments</h3>
          <div class="detail-stack">
            ${
              state.app.experiments.length
                ? state.app.experiments
                    .slice(0, 4)
                    .map(
                      (experiment) => `
                        <div class="detail-card">
                          <strong>${escapeHTML(experiment.id)}</strong>
                          <span>${escapeHTML(experiment.title)} · ${escapeHTML(experiment.creator || "unknown")}</span>
                        </div>
                      `,
                    )
                    .join("")
                : '<div class="detail-card"><strong>No experiments</strong><span>Create one from the action bar.</span></div>'
            }
          </div>
        </section>
      </aside>
    </div>
    ${state.status ? `<div class="status ${escapeHTML(state.status.kind)}">${escapeHTML(state.status.text)}</div>` : ""}
    ${renderModal()}
  `;

  root.querySelectorAll("[data-channel-id]").forEach((button) => {
    button.addEventListener("click", async () => {
      state.selectedChannel = button.dataset.channelId || "";
      await refreshState(state.selectedChannel);
    });
  });

  root.querySelectorAll("[data-open-modal]").forEach((button) => {
    button.addEventListener("click", () => openModal(button.dataset.openModal));
  });

  root.querySelectorAll("[data-close-modal]").forEach((button) => {
    button.addEventListener("click", closeModal);
  });

  root.querySelectorAll("[data-modal-field]").forEach((field) => {
    field.addEventListener("input", (event) => {
      if (!state.modal) return;
      state.modal.values[event.target.dataset.modalField] = event.target.value;
    });
  });

  root.querySelectorAll("[data-submit-modal]").forEach((button) => {
    button.addEventListener("click", submitModal);
  });

  const subjectInput = root.querySelector("#subject");
  const bodyInput = root.querySelector("#body");
  const replyInput = root.querySelector("#replyTo");
  const experimentInput = root.querySelector("#experimentID");
  if (subjectInput) subjectInput.addEventListener("input", (event) => { state.draft.subject = event.target.value; });
  if (bodyInput) bodyInput.addEventListener("input", (event) => { state.draft.body = event.target.value; });
  if (replyInput) replyInput.addEventListener("input", (event) => { state.draft.replyTo = event.target.value; });
  if (experimentInput) experimentInput.addEventListener("input", (event) => { state.draft.experimentID = event.target.value; });

  root.querySelectorAll("[data-refresh]").forEach((button) => {
    button.addEventListener("click", async () => {
      try {
        await refreshState(state.selectedChannel);
        showStatus("success", "State refreshed");
      } catch (err) {
        showStatus("error", err.message || String(err));
      }
    });
  });

  root.querySelectorAll("[data-send]").forEach((button) => {
    button.addEventListener("click", submitMessage);
  });
};

const bootstrap = async () => {
  render();
  try {
    await refreshState("");
  } catch (err) {
    showStatus("error", err.message || String(err));
    document.querySelector("#app").innerHTML = `<div class="empty-state">Failed to load GitChat desktop: ${escapeHTML(err.message || String(err))}</div>`;
  }
};

window.addEventListener("DOMContentLoaded", bootstrap);
