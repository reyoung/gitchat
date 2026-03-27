const api = () => window.go?.gui?.Bridge;

const state = {
  app: null,
  selectedChannel: "",
  selectedThread: "",
  attachmentCache: new Map(),
  previewOpen: false,
  sidebarOpen: true,
  autoRefreshTimer: null,
  composerHeight: 160,
  messageScrollBottomOffset: 0,
  draft: {
    body: "",
    replyTo: "",
    editOf: "",
    experimentSHA: "",
    pendingImages: [],
  },
  modal: null,
  confirmDelete: "",
  status: null,
  send: {
    inFlight: false,
    error: "",
    operation: "",
    detail: "",
    retryAction: "",
    retryPayload: null,
  },
};

const previousStateForScroll = {
  restoreAttempted: false,
  channelID: "",
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

const pendingImageURI = (id) => `gitchat-pending://${id}`;

const makePendingImageID = () => `pending-${Date.now()}-${Math.random().toString(36).slice(2, 10)}`;

const findPendingImage = (id) => state.draft.pendingImages.find((image) => image.id === id);

const markdownBlocks = (value) => {
  const lines = String(value || "").replaceAll("\r\n", "\n").split("\n");
  const blocks = [];
  let paragraph = [];
  let listItems = [];
  let codeFence = null;
  const flushParagraph = () => {
    if (!paragraph.length) return;
    blocks.push({ type: "paragraph", text: paragraph.join("\n") });
    paragraph = [];
  };
  const flushList = () => {
    if (!listItems.length) return;
    blocks.push({ type: "list", items: [...listItems] });
    listItems = [];
  };
  for (const line of lines) {
    if (line.startsWith("```")) {
      if (codeFence) {
        blocks.push({ type: "code", language: codeFence, text: paragraph.join("\n") });
        paragraph = [];
        codeFence = null;
      } else {
        flushParagraph();
        flushList();
        codeFence = line.slice(3).trim();
      }
      continue;
    }
    if (codeFence !== null) {
      paragraph.push(line);
      continue;
    }
    if (!line.trim()) {
      flushParagraph();
      flushList();
      continue;
    }
    const listMatch = line.match(/^\s*[-*]\s+(.+)$/);
    if (listMatch) {
      flushParagraph();
      listItems.push(listMatch[1]);
      continue;
    }
    flushList();
    paragraph.push(line);
  }
  flushList();
  if (paragraph.length) {
    blocks.push({ type: codeFence !== null ? "code" : "paragraph", language: codeFence || "", text: paragraph.join("\n") });
  }
  return blocks;
};

const renderInlineMarkdown = (value) => {
  let html = escapeHTML(value);
  html = html.replace(/!\[([^\]]*)\]\(([^)\s]+)\)/g, (_, alt, src) => {
    const safeAlt = escapeHTML(alt);
    if (src.startsWith("gitchat-pending://")) {
      const pending = findPendingImage(src.replace("gitchat-pending://", ""));
      if (!pending) return `<span class="pending-image-missing">${safeAlt || "Pending image"}</span>`;
      return `<img class="message-image pending-image" src="${escapeHTML(pending.dataURL)}" alt="${safeAlt}" />`;
    }
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
    if (block.type === "list") {
      return `<ul>${block.items.map((item) => `<li>${renderInlineMarkdown(item)}</li>`).join("")}</ul>`;
    }
    return `<p>${renderInlineMarkdown(block.text)}</p>`;
  })
  .join("") || `<p>${renderInlineMarkdown(value || "")}</p>`;

const syncComposerPreview = (root = document) => {
  const preview = root.querySelector(".composer-preview-body");
  if (!preview) return;
  preview.innerHTML = state.draft.body.trim()
    ? renderMarkdown(state.draft.body)
    : '<div class="composer-preview-empty">Markdown preview appears here.</div>';
  hydrateRenderedMarkdown(preview);
};

const showStatus = (kind, text) => {
  state.status = { kind, text };
  render();
  window.clearTimeout(showStatus.timer);
  showStatus.timer = window.setTimeout(() => {
    state.status = null;
    render();
  }, 3200);
};

const call = async (method, payload, options = {}) => {
  const bridge = api();
  if (!bridge || typeof bridge[method] !== "function") {
    throw new Error("Wails bridge is not available yet");
  }
  const timeoutMs = options.timeoutMs ?? 5000;
  const invoke = () => (payload === undefined ? bridge[method]() : bridge[method](payload));
  if (timeoutMs <= 0) {
    return invoke();
  }
  return Promise.race([
    invoke(),
    new Promise((_, reject) => {
      window.setTimeout(() => reject(new Error(`Timed out calling ${method}`)), timeoutMs);
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

const appendMarkdownToDraft = (markdown) => {
  if (!markdown) return;
  const needsLeadingBreak = state.draft.body && !state.draft.body.endsWith("\n");
  state.draft.body += `${needsLeadingBreak ? "\n" : ""}${markdown}\n`;
  render();
};

const readFileAsDataURL = async (file) => {
  const reader = new FileReader();
  return new Promise((resolve, reject) => {
    reader.onerror = () => reject(new Error("Failed to read pasted image"));
    reader.onload = () => resolve(String(reader.result || ""));
    reader.readAsDataURL(file);
  });
};

const uploadPastedImageFile = async (file) => {
  const dataURL = await readFileAsDataURL(file);
  const pendingImage = {
    id: makePendingImageID(),
    filename: file.name || "pasted-image.png",
    dataURL,
  };
  state.draft.pendingImages.push(pendingImage);
  appendMarkdownToDraft(`![${pendingImage.filename}](${pendingImageURI(pendingImage.id)})`);
  showStatus("success", "Image added to draft");
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
  const previousState = state.app;
  const messagesEl = document.querySelector(".messages");
  if (messagesEl) {
    state.messageScrollBottomOffset = Math.max(0, messagesEl.scrollHeight - messagesEl.scrollTop - messagesEl.clientHeight);
  }
  const appState = await call("GetState", selectedChannel || "");
  state.app = appState;
  state.selectedChannel = appState.selectedChannel || "";
  maybeNotifyNewMessages(previousState, appState);
  if (state.selectedThread) {
    const visible = getVisibleMessages(appState.messages || []);
    if (!visible.some((message) => message.commitHash === state.selectedThread)) {
      state.selectedThread = "";
    }
  }
  render();
};

const maybeNotifyNewMessages = async (previousState, nextState) => {
  if (!previousState || previousState.selectedChannel !== nextState.selectedChannel) return;
  const previousHashes = new Set((previousState.messages || []).map((message) => message.commitHash));
  const unseen = (nextState.messages || []).filter((message) => !previousHashes.has(message.commitHash) && message.userID !== nextState.currentUser);
  if (!unseen.length) return;
  const latest = unseen[unseen.length - 1];
  try {
    await call("NotifyNewMessages", {
      channelID: nextState.selectedChannel,
      userID: latest.userID,
      body: latest.body,
      count: unseen.length,
    }, { timeoutMs: 0 });
  } catch {
    // Ignore notification failures so polling stays healthy.
  }
};

const buildSendPayload = () => ({
  userID: state.app?.currentUser || "",
  channelID: state.selectedChannel,
  subject: "",
  body: state.draft.body,
  replyTo: state.draft.replyTo || (!state.draft.editOf && state.selectedThread ? state.selectedThread : ""),
  editOf: state.draft.editOf,
  deleteOf: "",
  experimentID: "",
  experimentSHA: state.draft.experimentSHA,
});

const buildDraftSnapshot = () => ({
  payload: buildSendPayload(),
  pendingImages: state.draft.pendingImages.map((image) => ({ ...image })),
});

const extractPendingImageIDs = (body) => {
  const matches = body.matchAll(/gitchat-pending:\/\/([a-z0-9-]+)/gi);
  return [...new Set(Array.from(matches, (match) => match[1]))];
};

const extractAttachmentURI = (markdown) => {
  const match = String(markdown || "").match(/!\[[^\]]*\]\(([^)\s]+)\)/);
  if (!match) {
    throw new Error("Failed to resolve uploaded image");
  }
  return match[1];
};

const resolvePendingImagesForPayload = async (payload, pendingImages) => {
  const imagesByID = new Map(pendingImages.map((image) => [image.id, image]));
  let resolvedPayload = { ...payload };
  const pendingIDs = extractPendingImageIDs(resolvedPayload.body);
  for (let index = 0; index < pendingIDs.length; index += 1) {
    const pendingID = pendingIDs[index];
    const pendingImage = imagesByID.get(pendingID);
    if (!pendingImage) {
      throw new Error("Draft image is missing from memory");
    }
    setSendState({
      detail: `Uploading image ${index + 1}/${pendingIDs.length}…`,
      retryPayload: {
        payload: resolvedPayload,
        pendingImages: pendingImages.filter((image) => image.id !== pendingID),
      },
    });
    render();
    const markdown = await call("UploadPastedImage", {
      userID: resolvedPayload.userID,
      channelID: resolvedPayload.channelID,
      filename: pendingImage.filename,
      dataURL: pendingImage.dataURL,
    }, { timeoutMs: 0 });
    const attachmentURI = extractAttachmentURI(markdown);
    resolvedPayload = {
      ...resolvedPayload,
      body: resolvedPayload.body.replaceAll(pendingImageURI(pendingID), attachmentURI),
    };
  }
  return resolvedPayload;
};

const setSendState = (next) => {
  state.send = {
    ...state.send,
    ...next,
  };
};

const submitMessage = async (draftSnapshot = buildDraftSnapshot()) => {
  if (state.send.inFlight) return;
  let currentSnapshot = {
    payload: { ...draftSnapshot.payload },
    pendingImages: (draftSnapshot.pendingImages || []).map((image) => ({ ...image })),
  };
  try {
    setSendState({
      inFlight: true,
      error: "",
      operation: "send",
      detail: `Sending to #${state.selectedChannel || "channel"}…`,
      retryAction: "send",
      retryPayload: currentSnapshot,
    });
    render();
    currentSnapshot.payload = await resolvePendingImagesForPayload(currentSnapshot.payload, currentSnapshot.pendingImages);
    setSendState({
      detail: `Sending to #${state.selectedChannel || "channel"}…`,
      retryPayload: currentSnapshot,
    });
    render();
    const appState = await call("SendMessage", currentSnapshot.payload, { timeoutMs: 0 });
    state.app = appState;
    state.selectedChannel = appState.selectedChannel || "";
    state.draft.body = "";
    state.draft.replyTo = "";
    state.draft.editOf = "";
    state.draft.experimentSHA = "";
    state.draft.pendingImages = [];
    setSendState({
      inFlight: false,
      error: "",
      operation: "",
      detail: "",
      retryAction: "",
      retryPayload: null,
    });
    render();
    showStatus("success", "Message saved");
  } catch (err) {
    setSendState({
      inFlight: false,
      error: err.message || String(err),
      operation: "",
      detail: "",
      retryAction: "send",
      retryPayload: currentSnapshot,
    });
    render();
  }
};

const retrySend = async () => {
  if (state.send.retryAction === "delete") {
    await deleteMessage(state.send.retryPayload);
    return;
  }
  if (!state.send.retryPayload) return;
  await submitMessage(state.send.retryPayload);
};

const deleteMessage = async (commitHash) => {
  if (!commitHash || state.send.inFlight) return;
  try {
    setSendState({
      inFlight: true,
      error: "",
      operation: "delete",
      detail: "Deleting message…",
      retryAction: "delete",
      retryPayload: commitHash,
    });
    render();
    const appState = await call("DeleteMessage", {
      userID: state.app?.currentUser || "",
      channelID: state.selectedChannel,
      commitHash,
    }, { timeoutMs: 0 });
    state.app = appState;
    state.selectedChannel = appState.selectedChannel || "";
    setSendState({
      inFlight: false,
      error: "",
      operation: "",
      detail: "",
      retryAction: "",
      retryPayload: null,
    });
    render();
    showStatus("success", "Message deleted");
  } catch (err) {
    setSendState({
      inFlight: false,
      error: err.message || String(err),
      operation: "",
      detail: "",
      retryAction: "delete",
      retryPayload: commitHash,
    });
    render();
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
      {
        name: "visibility",
        label: "Visibility",
        value: () => "public",
        options: [
          { value: "public", label: "Public" },
          { value: "private", label: "Private" },
        ],
      },
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

const openDeleteConfirm = (commitHash) => {
  state.confirmDelete = commitHash || "";
  render();
};

const closeDeleteConfirm = () => {
  state.confirmDelete = "";
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

const submitDeleteConfirm = async () => {
  const commitHash = state.confirmDelete;
  state.confirmDelete = "";
  render();
  await deleteMessage(commitHash);
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
    const appState = await call("UpdateAvatar", { userID: state.app?.currentUser || "" }, { timeoutMs: 0 });
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
  const latestDeleteByTarget = new Map();
  for (const message of messages) {
    if (message.editOf) {
      editCounts.set(message.editOf, (editCounts.get(message.editOf) || 0) + 1);
      latestEditByTarget.set(message.editOf, message);
    }
    if (message.deleteOf) {
      latestDeleteByTarget.set(message.deleteOf, message);
    }
  }
  return messages
    .filter((message) => !message.editOf && !message.deleteOf)
    .map((message) => {
      const latestEdit = latestEditByTarget.get(message.commitHash);
      const latestDelete = latestDeleteByTarget.get(message.commitHash);
      return {
        ...message,
        body: latestDelete ? "" : latestEdit ? latestEdit.body : message.body,
        subject: latestDelete ? "message deleted" : latestEdit ? latestEdit.subject : message.subject,
        editedAt: latestEdit ? latestEdit.createdAt : "",
        editCount: editCounts.get(message.commitHash) || 0,
        deleted: Boolean(latestDelete),
        deletedAt: latestDelete ? latestDelete.createdAt : "",
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

const dateLabelForMessage = (message) => {
  const raw = String(message?.createdAt || "").trim();
  if (!raw) return "";
  const [datePart] = raw.split(" ");
  return datePart || raw;
};

const renderDateDivider = (label) => `
  <div class="message-divider">
    <div class="message-divider-line"></div>
    <div class="message-divider-label">${escapeHTML(label)}</div>
    <div class="message-divider-line"></div>
  </div>
`;

const renderMessageCard = (message, options = {}) => {
  const mine = message.userID === state.app.currentUser ? " mine" : "";
  const reply = message.replyTo ? `<span class="message-thread subtle">reply to ${escapeHTML(message.replyTo.slice(0, 10))}</span>` : "";
  const edited = message.editCount ? `<span class="message-thread">edited ${message.editCount}x${message.editedAt ? ` · last ${escapeHTML(message.editedAt)}` : ""}</span>` : "";
  const deleted = message.deleted ? `<span class="message-thread">deleted${message.deletedAt ? ` · ${escapeHTML(message.deletedAt)}` : ""}</span>` : "";
  const threadSummary = options.threadSummary || null;
  const tags = [];
  if (message.experimentID) {
    const sha = message.experimentSHA ? ` · ${escapeHTML(message.experimentSHA)}` : "";
    tags.push(`<span class="tag experiment">experiment ${escapeHTML(message.experimentID)}${sha}</span>`);
  }
  const actions = [];
  if (message.userID === state.app.currentUser && !message.deleted) {
    actions.push(`<button type="button" class="message-action icon" title="Edit" aria-label="Edit" data-edit-hash="${escapeHTML(message.commitHash)}">✎</button>`);
    actions.push(`<button type="button" class="message-action icon danger" title="Delete" aria-label="Delete" data-delete-hash="${escapeHTML(message.commitHash)}">×</button>`);
  }
  if (!options.hideThreadButton) {
    actions.push(`<button type="button" class="message-action icon" title="Open thread" aria-label="Open thread" data-open-thread="${escapeHTML(message.commitHash)}">›</button>`);
  }
  if (message.deleted) {
    return `
      <article class="message-row${mine} deleted-line">
        <div class="deleted-line-rule"></div>
        <div class="deleted-line-main">
          <span class="deleted-line-label">Deleted</span>
          <div class="message-actions">${actions.join("")}</div>
        </div>
      </article>
    `;
  }
  return `
    <article class="message-row${mine}">
      <div class="message-avatar-wrap">
        ${renderAvatar(message.avatarURL, message.userID)}
      </div>
      <div class="message-content">
        <div class="message-head">
          <span class="message-author">${escapeHTML(message.userID)}</span>
          <span class="message-time">${escapeHTML(message.createdAt)}</span>
          <span class="message-sha" title="${escapeHTML(message.commitHash)}">${escapeHTML(message.shortHash)}</span>
          ${reply}
          ${edited}
          ${deleted}
        </div>
        <div class="message-actions">${actions.join("")}</div>
        <div class="message-body markdown-body">${renderMarkdown(message.body || "")}</div>
        ${tags.length ? `<div class="message-tags">${tags.join("")}</div>` : ""}
        ${threadSummary ? `<button type="button" class="thread-summary" data-open-thread="${escapeHTML(message.commitHash)}">${escapeHTML(threadSummary)}</button>` : ""}
      </div>
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
        ${threadMessages
          .map((message, index) => {
            const divider = index === 0 || dateLabelForMessage(threadMessages[index - 1]) !== dateLabelForMessage(message)
              ? renderDateDivider(dateLabelForMessage(message))
              : "";
            return divider + renderMessageCard(message, { hideThreadButton: true });
          })
          .join("")}
      </div>
    `;
  }
  const roots = getThreadRoots(visibleMessages);
  return roots
    .map((message, index) => {
      const threadMessages = getThreadMessages(visibleMessages, message.commitHash);
      const replies = Math.max(0, threadMessages.length - 1);
      const lastReply = replies > 0 ? threadMessages[threadMessages.length - 1].createdAt : "";
      const divider = index === 0 || dateLabelForMessage(roots[index - 1]) !== dateLabelForMessage(message)
        ? renderDateDivider(dateLabelForMessage(message))
        : "";
      const threadSummary = replies > 0 ? `${replies} repl${replies === 1 ? "y" : "ies"} · last ${lastReply}` : "";
      return divider + renderMessageCard(message, { threadSummary });
    })
    .join("");
};

const renderModal = () => {
  if (!state.modal) return "";
  const preset = modalPresets[state.modal.kind];
  const fields = preset.fields
    .map((field) => {
      const value = state.modal.values[field.name] || "";
      if (field.options) {
        const options = field.options
          .map((option) => `<option value="${escapeHTML(option.value)}" ${option.value === value ? "selected" : ""}>${escapeHTML(option.label)}</option>`)
          .join("");
        return `
          <div class="field">
            <label>${escapeHTML(field.label)}</label>
            <select data-modal-field="${escapeHTML(field.name)}">${options}</select>
          </div>
        `;
      }
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

const renderDeleteConfirm = () => {
  if (!state.confirmDelete) return "";
  return `
    <div class="modal-backdrop" data-close-delete="true">
      <div class="modal" onclick="event.stopPropagation()">
        <div class="modal-header">
          <h2>Delete message</h2>
          <p>This appends a delete commit. History is preserved.</p>
        </div>
        <div class="modal-body">
          <div class="field">
            <label>Commit</label>
            <input value="${escapeHTML(state.confirmDelete.slice(0, 12))}" readonly />
          </div>
        </div>
        <div class="modal-actions">
          <button type="button" data-close-delete="true">Cancel</button>
          <button type="button" class="primary" data-submit-delete="true">Delete</button>
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
  const railChannels = state.app.channels
    .map((channel) => `
      <button
        class="rail-channel ${channel.id === state.selectedChannel ? "active" : ""}"
        data-channel-id="${escapeHTML(channel.id)}"
        title="# ${escapeHTML(channel.id)}"
      >
        <span>#</span>
        <span>${escapeHTML(channel.id).slice(0, 2).toUpperCase()}</span>
      </button>
    `)
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
  const previewToggleLabel = state.previewOpen ? "Hide preview" : "Show preview";
  const composerClass = state.previewOpen ? "composer-split preview-open" : "composer-split";
  const pendingImageCount = state.draft.pendingImages.length;
  const composerHint = pendingImageCount > 0
    ? `${pendingImageCount} image${pendingImageCount > 1 ? "s" : ""} ready to upload on send. Paste images. Cmd+Enter to send.`
    : "Markdown supported. Paste images. Cmd+Enter to send. Replies, edits, and attachment uploads are appended as new commits.";
  const sendInlineStatus = state.send.inFlight
    ? `
      <div class="send-status send-status-progress">
        <div class="send-progress-track"><div class="send-progress-bar"></div></div>
        <div class="send-status-text">${escapeHTML(state.send.detail || (state.send.operation === "delete" ? "Deleting message…" : `Sending to #${state.selectedChannel || "channel"}…`))}</div>
      </div>
    `
    : state.send.error
    ? `
      <div class="send-status send-status-error">
        <div class="send-status-text">${escapeHTML(state.send.error)}</div>
        <button type="button" data-retry-send="true">Retry ${state.send.retryAction === "delete" ? "delete" : "send"}</button>
      </div>
    `
    : "";

  root.innerHTML = `
    <div class="shell ${state.sidebarOpen ? "" : "sidebar-collapsed"}">
      <aside class="workspace-rail">
        <button class="workspace-badge" data-update-avatar="true">${renderAvatar(state.app.currentUserAvatarURL, state.app.currentUser || "GC", "workspace-avatar")}</button>
        <button class="rail-toggle" type="button" data-toggle-sidebar="true" title="${state.sidebarOpen ? "Hide channels" : "Show channels"}">${state.sidebarOpen ? "◀" : "▶"}</button>
        ${state.sidebarOpen ? "" : `<div class="rail-channels">${railChannels}</div>`}
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
            ${state.app.selectedChannelIsPublic ? "" : '<button data-open-modal="addMember">Add member</button>'}
          </div>
        </header>
        <section class="messages">${renderMessages()}</section>
        <section class="composer">
          <div class="composer-resizer" data-composer-resizer="true" title="Drag to resize editor"></div>
          <div class="composer-card">
            ${replyBanner}
            <div class="composer-toolbar">
            </div>
            <div class="${composerClass}">
              <div class="field composer-editor">
                <textarea id="body" style="height:${Math.max(160, state.composerHeight)}px" placeholder="Write a message. Use Cmd+Enter to send. Markdown and images are supported.">${escapeHTML(state.draft.body)}</textarea>
              </div>
              <div class="composer-preview ${state.previewOpen ? "is-open" : ""}">
                <div class="composer-preview-body markdown-body" style="height:${Math.max(160, state.composerHeight)}px">${state.draft.body.trim() ? renderMarkdown(state.draft.body) : '<div class="composer-preview-empty">Markdown preview appears here.</div>'}</div>
              </div>
            </div>
            <div class="composer-actions">
              <div class="composer-meta">
                <button type="button" data-toggle-preview="true">${previewToggleLabel}</button>
                <div class="hint">${escapeHTML(composerHint)}</div>
                ${sendInlineStatus}
              </div>
              <button class="primary" data-send="true" ${state.send.inFlight ? "disabled" : ""}>${state.send.inFlight ? (state.send.operation === "delete" ? "Deleting…" : "Sending…") : sendLabel}</button>
            </div>
          </div>
        </section>
      </main>
    </div>
    ${state.status ? `<div class="status ${escapeHTML(state.status.kind)}">${escapeHTML(state.status.text)}</div>` : ""}
    ${renderModal()}
    ${renderDeleteConfirm()}
  `;

  root.querySelectorAll("[data-channel-id]").forEach((button) => {
    button.addEventListener("click", async () => {
      state.selectedChannel = button.dataset.channelId || "";
      await refreshState(state.selectedChannel);
    });
  });

  root.querySelectorAll("[data-toggle-sidebar]").forEach((button) => {
    button.addEventListener("click", () => {
      state.sidebarOpen = !state.sidebarOpen;
      render();
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

  root.querySelectorAll("[data-close-delete]").forEach((button) => {
    button.addEventListener("click", closeDeleteConfirm);
  });

  root.querySelectorAll("[data-modal-field]").forEach((field) => {
    const syncField = (event) => {
      if (!state.modal) return;
      state.modal.values[event.target.dataset.modalField] = event.target.value;
    };
    field.addEventListener("input", syncField);
    field.addEventListener("change", syncField);
  });

  root.querySelectorAll("[data-submit-modal]").forEach((button) => {
    button.addEventListener("click", submitModal);
  });

  root.querySelectorAll("[data-submit-delete]").forEach((button) => {
    button.addEventListener("click", submitDeleteConfirm);
  });

  const bodyInput = root.querySelector("#body");
  if (bodyInput) bodyInput.addEventListener("input", (event) => {
    state.draft.body = event.target.value;
    syncComposerPreview(root);
  });
  if (bodyInput) bodyInput.addEventListener("keydown", async (event) => {
    if ((event.metaKey || event.ctrlKey) && event.key === "Enter") {
      event.preventDefault();
      await submitMessage();
    }
  });
  if (bodyInput) bodyInput.addEventListener("paste", async (event) => {
    const items = Array.from(event.clipboardData?.items || []);
    const imageItem = items.find((item) => item.type.startsWith("image/"));
    if (!imageItem) return;
    if (!state.selectedChannel) {
      showStatus("error", "Select a channel before pasting an image");
      return;
    }
    const file = imageItem.getAsFile();
    if (!file) return;
    event.preventDefault();
    try {
      await uploadPastedImageFile(file);
    } catch (err) {
      showStatus("error", err.message || String(err));
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

  root.querySelectorAll("[data-retry-send]").forEach((button) => {
    button.addEventListener("click", retrySend);
  });

  root.querySelectorAll("[data-toggle-preview]").forEach((button) => {
    button.addEventListener("click", () => {
      state.previewOpen = !state.previewOpen;
      render();
    });
  });

  root.querySelectorAll("[data-reply-hash]").forEach((button) => {
    button.addEventListener("click", () => startReply(button.dataset.replyHash || ""));
  });

  root.querySelectorAll("[data-edit-hash]").forEach((button) => {
    button.addEventListener("click", () => startEdit(button.dataset.editHash || ""));
  });

  root.querySelectorAll("[data-delete-hash]").forEach((button) => {
    button.addEventListener("click", () => {
      const commitHash = button.dataset.deleteHash || "";
      if (!commitHash) return;
      openDeleteConfirm(commitHash);
    });
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

  root.querySelectorAll("[data-composer-resizer]").forEach((handle) => {
    handle.addEventListener("mousedown", (event) => {
      event.preventDefault();
      const startY = event.clientY;
      const startHeight = state.composerHeight;
      const onMove = (moveEvent) => {
        const delta = startY - moveEvent.clientY;
        state.composerHeight = Math.max(160, Math.min(window.innerHeight * 0.55, Math.round(startHeight + delta)));
        const editor = document.querySelector("#body");
        if (editor) {
          editor.style.height = `${state.composerHeight}px`;
        }
      };
      const onUp = () => {
        window.removeEventListener("mousemove", onMove);
        window.removeEventListener("mouseup", onUp);
      };
      window.addEventListener("mousemove", onMove);
      window.addEventListener("mouseup", onUp);
    });
  });

  hydrateRenderedMarkdown(root);
  syncComposerPreview(root);
  const nextMessagesEl = root.querySelector(".messages");
  if (nextMessagesEl) {
    const wasAtBottom = state.messageScrollBottomOffset <= 48;
    const channelChanged = previousStateForScroll.channelID !== state.selectedChannel;
    if (!previousStateForScroll.restoreAttempted || channelChanged) {
      nextMessagesEl.scrollTop = nextMessagesEl.scrollHeight;
    } else if (wasAtBottom) {
      nextMessagesEl.scrollTop = nextMessagesEl.scrollHeight;
    } else {
      nextMessagesEl.scrollTop = Math.max(0, nextMessagesEl.scrollHeight - nextMessagesEl.clientHeight - state.messageScrollBottomOffset);
    }
    previousStateForScroll.restoreAttempted = true;
    previousStateForScroll.channelID = state.selectedChannel;
  }
  const channelListEl = root.querySelector(".channel-list");
  if (channelListEl) {
    channelListEl.scrollTop = channelListEl.scrollHeight;
  }
};

const bootstrap = async () => {
  render();
  try {
    await waitForBridge();
    await refreshState("");
    if (!state.autoRefreshTimer) {
      state.autoRefreshTimer = window.setInterval(() => {
        refreshState(state.selectedChannel).catch(() => {});
      }, 10000);
    }
  } catch (err) {
    showStatus("error", err.message || String(err));
    document.querySelector("#app").innerHTML = `<div class="empty-state">Failed to load GitChat desktop: ${escapeHTML(err.message || String(err))}</div>`;
  }
};

window.addEventListener("DOMContentLoaded", bootstrap);
