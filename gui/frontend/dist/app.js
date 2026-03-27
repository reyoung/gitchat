const api = () => window.go?.gui?.Bridge;

const state = {
  app: null,
  selectedChannel: "",
  selectedThread: "",
  attachmentCache: new Map(),
  draft: {
    body: "",
    replyTo: "",
    editOf: "",
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

const initialsForUser = (userID) => {
  const cleaned = String(userID || "").trim();
  if (!cleaned) return "GC";
  return cleaned.slice(0, 2).toUpperCase();
};

const renderAvatar = (avatarURL, fallback, className = "avatar") => {
  if (avatarURL) {
    if (avatarURL.startsWith("gitchat-attachment://")) {
      try {
        const url = new URL(avatarURL);
        const commitHash = escapeHTML(url.hostname);
        const path = escapeHTML(url.searchParams.get("path") || "");
        return `<img class="${className}" data-attachment-commit="${commitHash}" data-attachment-path="${path}" alt="${escapeHTML(fallback)}" />`;
      } catch {
        return `<div class="${className} fallback">${escapeHTML(initialsForUser(fallback))}</div>`;
      }
    }
    return `<img class="${className}" src="${escapeHTML(avatarURL)}" alt="${escapeHTML(fallback)}" />`;
  }
  return `<div class="${className} fallback">${escapeHTML(initialsForUser(fallback))}</div>`;
};

const markdownBlocks = (value) => {
  const lines = String(value || "").replaceAll("\r\n", "\n").split("\n");
  const blocks = [];
  let paragraph = [];
  let codeFence = null;
  for (const line of lines) {
    if (line.startsWith("```")) {
      if (codeFence) {
        blocks.push({ type: "code", language: codeFence, text: paragraph.join("\n") });
        paragraph = [];
        codeFence = null;
      } else {
        if (paragraph.length) {
          blocks.push({ type: "paragraph", text: paragraph.join("\n") });
          paragraph = [];
        }
        codeFence = line.slice(3).trim();
      }
      continue;
    }
    if (codeFence !== null) {
      paragraph.push(line);
      continue;
    }
    if (!line.trim()) {
      if (paragraph.length) {
        blocks.push({ type: "paragraph", text: paragraph.join("\n") });
        paragraph = [];
      }
      continue;
    }
    paragraph.push(line);
  }
  if (paragraph.length) {
    blocks.push({ type: codeFence !== null ? "code" : "paragraph", language: codeFence || "", text: paragraph.join("\n") });
  }
  return blocks;
};

const renderInlineMarkdown = (value) => {
  let html = escapeHTML(value);
  html = html.replace(/!\[([^\]]*)\]\(([^)\s]+)\)/g, (_, alt, src) => {
    const safeAlt = escapeHTML(alt);
    if (src.startsWith("gitchat-attachment://")) {
      try {
        const url = new URL(src);
        const commitHash = escapeHTML(url.hostname);
        const path = escapeHTML(url.searchParams.get("path") || "");
        return `<img class="message-image" data-attachment-commit="${commitHash}" data-attachment-path="${path}" alt="${safeAlt}" />`;
      } catch {
        return "";
      }
    }
    return `<img class="message-image" src="${escapeHTML(src)}" alt="${safeAlt}" />`;
  });
  html = html.replace(/\[([^\]]+)\]\((https?:\/\/[^)\s]+)\)/g, '<a href="$2" class="message-link" target="_blank" rel="noreferrer">$1</a>');
  html = html.replace(/\*\*([^*]+)\*\*/g, "<strong>$1</strong>");
  html = html.replace(/(^|[\s(])\*([^*]+)\*(?=[\s).,!?:;]|$)/g, "$1<em>$2</em>");
  html = html.replace(/`([^`]+)`/g, "<code>$1</code>");
  return html.replaceAll("\n", "<br />");
};

const renderMarkdown = (value) => markdownBlocks(value)
  .map((block) => {
    if (block.type === "code") {
      return `<pre class="message-code"><code>${escapeHTML(block.text)}</code></pre>`;
    }
    return `<p>${renderInlineMarkdown(block.text)}</p>`;
  })
  .join("") || `<p>${renderInlineMarkdown(value || "")}</p>`;

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
  return Promise.race([
    bridge[method](payload),
    new Promise((_, reject) => {
      window.setTimeout(() => reject(new Error(`Timed out calling ${method}`)), 5000);
    }),
  ]);
};

const resolveAttachmentImage = async (img) => {
  const commitHash = img.dataset.attachmentCommit || "";
  const path = img.dataset.attachmentPath || "";
  if (!commitHash || !path) return;
  const cacheKey = `${commitHash}:${path}`;
  if (state.attachmentCache.has(cacheKey)) {
    img.src = state.attachmentCache.get(cacheKey);
    return;
  }
  img.classList.add("loading");
  try {
    const dataURL = await call("ResolveAttachment", { commitHash, path });
    if (!dataURL) return;
    state.attachmentCache.set(cacheKey, dataURL);
    img.src = dataURL;
  } catch (err) {
    img.alt = err.message || "Failed to load image";
  } finally {
    img.classList.remove("loading");
  }
};

const hydrateRenderedMarkdown = (root) => {
  root.querySelectorAll("img[data-attachment-commit]").forEach((img) => {
    resolveAttachmentImage(img);
  });
};

const waitForBridge = async () => {
  const startedAt = Date.now();
  while (Date.now() - startedAt < 5000) {
    const bridge = api();
    if (bridge && typeof bridge.GetState === "function") {
      return;
    }
    await new Promise((resolve) => window.setTimeout(resolve, 50));
  }
  throw new Error("Wails bridge did not become ready");
};

const refreshState = async (selectedChannel = state.selectedChannel) => {
  const appState = await call("GetState", selectedChannel || "");
  state.app = appState;
  state.selectedChannel = appState.selectedChannel || "";
  if (state.selectedThread) {
    const visible = getVisibleMessages(appState.messages || []);
    if (!visible.some((message) => message.commitHash === state.selectedThread)) {
      state.selectedThread = "";
    }
  }
  render();
};

const submitMessage = async () => {
  try {
    const appState = await call("SendMessage", {
      userID: state.app?.currentUser || "",
      channelID: state.selectedChannel,
      subject: "",
      body: state.draft.body,
      replyTo: state.draft.replyTo,
      editOf: state.draft.editOf,
      experimentID: "",
      experimentSHA: state.draft.experimentSHA,
    });
    state.app = appState;
    state.selectedChannel = appState.selectedChannel || "";
    state.draft.body = "";
    state.draft.replyTo = "";
    state.draft.editOf = "";
    state.draft.experimentSHA = "";
    render();
    showStatus("success", "Message saved");
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

const startReply = (commitHash) => {
  state.draft.editOf = "";
  state.draft.replyTo = commitHash || "";
  render();
  const composer = document.querySelector("#body");
  if (composer) composer.focus();
};

const clearReply = () => {
  state.draft.replyTo = "";
  state.draft.editOf = "";
  render();
  const composer = document.querySelector("#body");
  if (composer) composer.focus();
};

const startEdit = (commitHash) => {
  const message = getVisibleMessages(state.app?.messages || []).find((item) => item.commitHash === commitHash);
  if (!message) return;
  state.draft.replyTo = "";
  state.draft.editOf = commitHash;
  state.draft.body = message.body || "";
  render();
  const composer = document.querySelector("#body");
  if (composer) composer.focus();
};

const clearComposerMode = () => {
  state.draft.replyTo = "";
  state.draft.editOf = "";
  render();
};

const openThread = (commitHash) => {
  state.selectedThread = commitHash || "";
  render();
};

const closeThread = () => {
  state.selectedThread = "";
  render();
};

const updateAvatar = async () => {
  try {
    const appState = await call("UpdateAvatar", { userID: state.app?.currentUser || "" });
    state.app = appState;
    render();
    showStatus("success", "Avatar updated");
  } catch (err) {
    showStatus("error", err.message || String(err));
  }
};

const getVisibleMessages = (messages) => {
  const latestEditByTarget = new Map();
  const editCounts = new Map();
  for (const message of messages) {
    if (!message.editOf) continue;
    editCounts.set(message.editOf, (editCounts.get(message.editOf) || 0) + 1);
    latestEditByTarget.set(message.editOf, message);
  }
  return messages
    .filter((message) => !message.editOf)
    .map((message) => {
      const latestEdit = latestEditByTarget.get(message.commitHash);
      return {
        ...message,
        body: latestEdit ? latestEdit.body : message.body,
        subject: latestEdit ? latestEdit.subject : message.subject,
        editedAt: latestEdit ? latestEdit.createdAt : "",
        editCount: editCounts.get(message.commitHash) || 0,
      };
    });
};

const getThreadRoot = (messageMap, message) => {
  let current = message;
  while (current?.replyTo && messageMap.has(current.replyTo)) {
    current = messageMap.get(current.replyTo);
  }
  return current?.commitHash || message.commitHash;
};

const getThreadRoots = (messages) => {
  const byHash = new Map(messages.map((message) => [message.commitHash, message]));
  const roots = messages.filter((message) => !message.replyTo || !byHash.has(message.replyTo));
  return roots;
};

const getThreadMessages = (messages, rootHash) => {
  const byHash = new Map(messages.map((message) => [message.commitHash, message]));
  return messages.filter((message) => getThreadRoot(byHash, message) === rootHash);
};

const renderMessageCard = (message, options = {}) => {
  const mine = message.userID === state.app.currentUser ? " mine" : "";
  const reply = message.replyTo ? `<span class="message-thread">reply to ${escapeHTML(message.replyTo.slice(0, 10))}</span>` : "";
  const edited = message.editCount ? `<span class="message-thread">edited ${message.editCount}x${message.editedAt ? ` · last ${escapeHTML(message.editedAt)}` : ""}</span>` : "";
  const tags = [];
  if (message.experimentID) {
    const sha = message.experimentSHA ? ` · ${escapeHTML(message.experimentSHA)}` : "";
    tags.push(`<span class="tag experiment">experiment ${escapeHTML(message.experimentID)}${sha}</span>`);
  }
  const actions = [
    `<button type="button" class="message-action" data-reply-hash="${escapeHTML(message.commitHash)}">Reply</button>`,
  ];
  if (message.userID === state.app.currentUser) {
    actions.push(`<button type="button" class="message-action" data-edit-hash="${escapeHTML(message.commitHash)}">Edit</button>`);
  }
  if (!options.hideThreadButton) {
    actions.push(`<button type="button" class="message-action" data-open-thread="${escapeHTML(message.commitHash)}">Thread</button>`);
  }
  return `
    <article class="message-card${mine}">
      <div class="message-top">
        ${renderAvatar(message.avatarURL, message.userID)}
        <div class="message-meta">
          <span class="message-author">${escapeHTML(message.userID)}</span>
          <span class="message-time">${escapeHTML(message.createdAt)}</span>
          <span class="message-sha">${escapeHTML(message.shortHash)}</span>
          ${reply}
          ${edited}
        </div>
      </div>
      <div class="message-body markdown-body">${renderMarkdown(message.body || "")}</div>
      ${tags.length ? `<div class="message-tags">${tags.join("")}</div>` : ""}
      <div class="message-actions">${actions.join("")}</div>
    </article>
  `;
};

const renderMessages = () => {
  const visibleMessages = getVisibleMessages(state.app?.messages || []);
  if (!visibleMessages.length) {
    return `
      <div class="empty-state">
        No messages yet. Start the thread in <strong>#${escapeHTML(state.selectedChannel || "channel")}</strong>.
      </div>
    `;
  }
  if (state.selectedThread) {
    const threadMessages = getThreadMessages(visibleMessages, state.selectedThread);
    if (!threadMessages.length) {
      state.selectedThread = "";
      return renderMessages();
    }
    return `
      <div class="thread-page">
        <div class="thread-header">
          <button type="button" class="thread-back" data-close-thread="true">Back to channel</button>
          <div class="thread-meta">${threadMessages.length} message${threadMessages.length === 1 ? "" : "s"} in thread</div>
        </div>
        ${threadMessages.map((message) => renderMessageCard(message, { hideThreadButton: true })).join("")}
      </div>
    `;
  }
  const roots = getThreadRoots(visibleMessages);
  return roots
    .map((message) => renderMessageCard(message))
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

  const selectedTitle = state.app.selectedChannelTitle || "Pick a channel";
  const selectedMeta = state.selectedChannel
    ? `Channel branch: channels/${escapeHTML(state.selectedChannel)}`
    : "Create a channel to start chatting";
  const replyBanner = state.draft.editOf
    ? `
      <div class="reply-banner">
        <span>Editing ${escapeHTML(state.draft.editOf.slice(0, 10))} as a new commit</span>
        <button type="button" data-clear-mode="true">Cancel edit</button>
      </div>
    `
    : state.draft.replyTo
    ? `
      <div class="reply-banner">
        <span>Replying to ${escapeHTML(state.draft.replyTo.slice(0, 10))}</span>
        <button type="button" data-clear-mode="true">Cancel reply</button>
      </div>
    `
    : "";
  const sendLabel = state.draft.editOf ? "Save edit" : "Send message";
  const canInsertImage = Boolean(state.selectedChannel);

  root.innerHTML = `
    <div class="shell">
      <aside class="workspace-rail">
        <button class="workspace-badge" data-update-avatar="true">${renderAvatar(state.app.currentUserAvatarURL, state.app.currentUser || "GC", "workspace-avatar")}</button>
        <div class="workspace-user">
          <div>GitChat</div>
          <div>${escapeHTML(state.app.currentUser || "unconfigured user")}</div>
        </div>
      </aside>

      <aside class="sidebar">
        <div class="sidebar-header">
          <h1 class="sidebar-title">GitChat</h1>
          <p class="sidebar-subtitle">Channels and threaded messages on top of git commits.</p>
        </div>
        <div class="sidebar-actions">
          <button class="primary" data-open-modal="createChannel">New channel</button>
        </div>
        <section class="sidebar-section">
          <div class="section-label">Channels</div>
          <div class="channel-list">${channels || '<div class="empty-state">No channels yet.</div>'}</div>
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
          </div>
        </header>
        <section class="messages">${renderMessages()}</section>
        <section class="composer">
          <div class="composer-card">
            ${replyBanner}
            <div class="composer-toolbar">
              <button type="button" data-insert-image="true" ${canInsertImage ? "" : "disabled"}>Insert image</button>
              <div class="hint">Markdown supported. Cmd+Enter to send.</div>
            </div>
            <div class="field" style="padding: 14px 14px 0;">
              <label>Message</label>
              <textarea id="body" placeholder="Write a message. Use Cmd+Enter to send. Markdown and images are supported.">${escapeHTML(state.draft.body)}</textarea>
            </div>
            <div class="composer-actions">
              <div class="hint">Replies, edits, and attachment uploads are appended as new commits.</div>
              <button class="primary" data-send="true">${sendLabel}</button>
            </div>
          </div>
        </section>
      </main>
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

  root.querySelectorAll("[data-update-avatar]").forEach((button) => {
    button.addEventListener("click", updateAvatar);
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

  const bodyInput = root.querySelector("#body");
  if (bodyInput) bodyInput.addEventListener("input", (event) => { state.draft.body = event.target.value; });
  if (bodyInput) bodyInput.addEventListener("keydown", async (event) => {
    if ((event.metaKey || event.ctrlKey) && event.key === "Enter") {
      event.preventDefault();
      await submitMessage();
    }
  });

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

  root.querySelectorAll("[data-insert-image]").forEach((button) => {
    button.addEventListener("click", async () => {
      try {
        const markdown = await call("InsertImage", {
          userID: state.app?.currentUser || "",
          channelID: state.selectedChannel,
        });
        if (!markdown) return;
        const composer = root.querySelector("#body");
        const insertion = `${state.draft.body && !state.draft.body.endsWith("\n") ? "\n" : ""}${markdown}\n`;
        state.draft.body += insertion;
        if (composer) {
          composer.value = state.draft.body;
          composer.focus();
        }
      } catch (err) {
        showStatus("error", err.message || String(err));
      }
    });
  });

  root.querySelectorAll("[data-reply-hash]").forEach((button) => {
    button.addEventListener("click", () => startReply(button.dataset.replyHash || ""));
  });

  root.querySelectorAll("[data-edit-hash]").forEach((button) => {
    button.addEventListener("click", () => startEdit(button.dataset.editHash || ""));
  });

  root.querySelectorAll("[data-open-thread]").forEach((button) => {
    button.addEventListener("click", () => openThread(button.dataset.openThread || ""));
  });

  root.querySelectorAll("[data-close-thread]").forEach((button) => {
    button.addEventListener("click", closeThread);
  });

  root.querySelectorAll("[data-clear-mode]").forEach((button) => {
    button.addEventListener("click", clearComposerMode);
  });

  hydrateRenderedMarkdown(root);
};

const bootstrap = async () => {
  render();
  try {
    await waitForBridge();
    await refreshState("");
  } catch (err) {
    showStatus("error", err.message || String(err));
    document.querySelector("#app").innerHTML = `<div class="empty-state">Failed to load GitChat desktop: ${escapeHTML(err.message || String(err))}</div>`;
  }
};

window.addEventListener("DOMContentLoaded", bootstrap);
