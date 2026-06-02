// State Management
const state = {
  token: localStorage.getItem("teampulse_token") || "",
  profile: null,
  currentTeamId: localStorage.getItem("teampulse_team_id") || "",
  teamsList: [],
  members: [],
  onlineMembers: [],
  tasks: [],
  notifications: [],
  unreadCount: 0,
  activities: [],
  hotTasks: [],
  selectedTask: null,
  timers: {
    heartbeat: null,
    online: null,
    polling: null
  }
};

// DOM Query Helper
const $ = (selector) => document.querySelector(selector);
const $$ = (selector) => document.querySelectorAll(selector);

// Constants mappings
const STATUS_LABELS = {
  todo: "待处理",
  doing: "进行中",
  done: "已完成"
};

const PRIORITY_LABELS = {
  1: "低",
  2: "中",
  3: "高"
};

// Toast Notifications Helper
function showToast(message, type = "info") {
  const container = $("#toastContainer");
  if (!container) return;

  const toast = document.createElement("div");
  toast.className = `toast ${type}`;
  
  let icon = "fa-circle-info";
  if (type === "success") icon = "fa-circle-check";
  if (type === "error") icon = "fa-triangle-exclamation";

  toast.innerHTML = `
    <i class="fa-solid ${icon} toast-icon"></i>
    <span>${message}</span>
  `;

  container.appendChild(toast);

  // Auto remove toast
  setTimeout(() => {
    toast.style.opacity = "0";
    toast.style.transform = "translateY(10px)";
    setTimeout(() => toast.remove(), 300);
  }, 4000);
}

// Write to operations trail and dev log
function setLog(method, url, payload, response, redisKey = "等待操作", isError = false) {
  const time = new Date().toLocaleTimeString();
  const rawLog = `[${time}] ${method} ${url}\nRequest: ${JSON.stringify(payload)}\nResponse: ${JSON.stringify(response, null, 2)}`;
  
  const logOutput = $("#logOutput");
  if (logOutput) {
    if (isError) {
      logOutput.innerHTML = `<span style="color: #ef4444;">${rawLog}</span>`;
    } else {
      logOutput.textContent = rawLog;
    }
  }

  const flowRedis = $("#flowRedis");
  if (flowRedis) {
    flowRedis.textContent = redisKey;
    // Highlight step temporarily
    const parent = flowRedis.closest(".flow-step");
    if (parent) {
      parent.classList.add("pulse-highlight");
      setTimeout(() => parent.classList.remove("pulse-highlight"), 1000);
    }
  }

  // Push trail logs
  const trailBody = $("#eventTrailBody");
  if (trailBody) {
    const item = document.createElement("div");
    item.className = "trail-item";
    item.innerHTML = `[${time}] <b>${method}</b> <span style="color: #a5b4fc;">${url}</span> &rarr; Key: <code style="color: #34d399;">${redisKey}</code>`;
    
    if (trailBody.querySelector(".text-muted")) {
      trailBody.innerHTML = "";
    }
    trailBody.prepend(item);

    // Limit to 5 logs
    while (trailBody.children.length > 5) {
      trailBody.lastChild.remove();
    }
  }
}

// Core Request Helper
async function request(path, options = {}) {
  const headers = {
    "Content-Type": "application/json",
    ...(options.headers || {})
  };

  if (state.token) {
    headers.Authorization = `Bearer ${state.token}`;
  }

  const fetchOptions = {
    ...options,
    headers
  };

  try {
    const response = await fetch(path, fetchOptions);
    const text = await response.text();
    let body = {};
    
    try {
      body = text ? JSON.parse(text) : {};
    } catch (e) {
      body = { message: text || response.statusText };
    }

    // Determine the probable Redis key involved based on endpoint path
    let redisKey = "N/A";
    if (path.includes("/auth/captcha")) redisKey = "auth:captcha:{username}";
    else if (path.includes("/auth/login")) redisKey = "auth:login:{ip}:{min}";
    else if (path.includes("/auth/logout")) redisKey = "jwt:blacklist:{token}";
    else if (path.includes("/user/profile")) redisKey = "user:profile:{userId}";
    else if (path.includes("/teams") && options.method === "POST" && !path.includes("/members") && !path.includes("/tasks")) redisKey = "team:members:{teamId}";
    else if (path.includes("/members")) redisKey = "team:members:{teamId}";
    else if (path.includes("/tasks/hot")) redisKey = "team:task:hot:{teamId}";
    else if (path.includes("/tasks") && options.method === "POST") redisKey = "team:activities:{teamId}";
    else if (path.match(/\/tasks\/\d+$/) && options.method === "GET") redisKey = "team:task:hot:{teamId} (ZINCRBY)";
    else if (path.match(/\/tasks\/\d+$/) && options.method === "PUT") redisKey = "team:activities:{teamId}";
    else if (path.includes("/status")) redisKey = "team:activities:{teamId}";
    else if (path.includes("/heartbeat")) redisKey = "presence:user & presence:team";
    else if (path.includes("/online-members")) redisKey = "presence:team:{teamId}";
    else if (path.includes("/notifications/unread-count")) redisKey = "notification:unread:{userId}";
    else if (path.includes("/notifications") && path.includes("/read")) redisKey = "notification:unread:{userId} (SREM)";
    else if (path.includes("/activities")) redisKey = "team:activities:{teamId} (LRANGE)";

    if (!response.ok || body.code) {
      // Token expiration check
      if (response.status === 401 || body.code === 401) {
        showToast("登录会话已过期，请重新登录", "error");
        logout();
      }
      setLog(options.method || "GET", path, options.body ? JSON.parse(options.body) : null, body, redisKey, true);
      throw body;
    }

    setLog(options.method || "GET", path, options.body ? JSON.parse(options.body) : null, body, redisKey, false);
    return body.data ?? body;
  } catch (error) {
    console.error("API Request Error:", error);
    throw error;
  }
}

// Set active authentication token
function setToken(token) {
  state.token = token || "";
  if (state.token) {
    localStorage.setItem("teampulse_token", state.token);
  } else {
    localStorage.removeItem("teampulse_token");
  }
  updateUIOnAuthState();
}

// Clear current session / Logout
function logout() {
  setToken("");
  state.profile = null;
  state.currentTeamId = "";
  localStorage.removeItem("teampulse_team_id");
  clearTimers();
  updateUIOnAuthState();
  $("#authOverlay").classList.add("active");
}

// Clear all active presence timers
function clearTimers() {
  if (state.timers.heartbeat) clearInterval(state.timers.heartbeat);
  if (state.timers.online) clearInterval(state.timers.online);
  if (state.timers.polling) clearInterval(state.timers.polling);
  state.timers.heartbeat = null;
  state.timers.online = null;
  state.timers.polling = null;
}

// Start timers for presence and logs
function startTimers() {
  clearTimers();
  if (!state.token || !state.currentTeamId) return;

  // 1. Online Heartbeat (30s)
  sendHeartbeat();
  state.timers.heartbeat = setInterval(sendHeartbeat, 30000);

  // 2. Refresh Online Teammates (15s)
  refreshOnlineList();
  state.timers.online = setInterval(refreshOnlineList, 15000);

  // 3. Background general data poll (30s) - Notification unreads, timeline
  state.timers.polling = setInterval(() => {
    refreshNotifications(false);
    refreshActivities(false);
    refreshHotTasks(false);
  }, 30000);
}

// Set active current team
function setCurrentTeam(teamId) {
  state.currentTeamId = String(teamId || "");
  if (state.currentTeamId) {
    localStorage.setItem("teampulse_team_id", state.currentTeamId);
  } else {
    localStorage.removeItem("teampulse_team_id");
  }
  
  // Sync selectors
  const selector = $("#teamSelector");
  if (selector) selector.value = state.currentTeamId;

  // Refresh lists
  if (state.currentTeamId) {
    startTimers();
    loadTeamData();
  } else {
    clearTimers();
  }
  
  updateMetricsUI();
}

// Perform team data fetch
async function loadTeamData() {
  if (!state.currentTeamId) return;
  try {
    await Promise.all([
      refreshTasks(),
      refreshMembers(),
      refreshOnlineList(),
      refreshActivities(),
      refreshHotTasks()
    ]);
  } catch (error) {
    console.error("Error loading team data:", error);
  }
}

// Refresh Joined/Available Teams
async function refreshTeamsDropdown() {
  // Check if we have teams first. GoFrame backend has a POST to create teams,
  // but doesn't have an explicit GET /teams list endpoint in the instructions.
  // We can maintain a list of team IDs in localStorage for fallback/switching,
  // and load details from the active context. Let's load the active ID if stored.
  const selectEl = $("#teamSelector");
  if (!selectEl) return;

  selectEl.innerHTML = '<option value="">-- 请选择团队 --</option>';
  
  // Store created teams inside local storage to mock list
  let localTeams = JSON.parse(localStorage.getItem("teampulse_saved_teams") || "[]");
  if (state.currentTeamId && !localTeams.some(t => t.id == state.currentTeamId)) {
    localTeams.push({ id: state.currentTeamId, name: `团队 #${state.currentTeamId}` });
    localStorage.setItem("teampulse_saved_teams", JSON.stringify(localTeams));
  }

  localTeams.forEach(team => {
    const opt = document.createElement("option");
    opt.value = team.id;
    opt.textContent = `${team.name} (ID: ${team.id})`;
    selectEl.appendChild(opt);
  });

  if (state.currentTeamId) {
    selectEl.value = state.currentTeamId;
  }
}

// Update DOM elements on Auth States
function updateUIOnAuthState() {
  const isAuth = !!state.token;
  document.body.classList.toggle("is-authenticated", isAuth);
  
  if (isAuth) {
    $("#authOverlay").classList.remove("active");
    // Load profile
    refreshUserProfile();
    refreshNotifications();
    refreshTeamsDropdown();
    if (state.currentTeamId) {
      setCurrentTeam(state.currentTeamId);
    }
  } else {
    $("#authOverlay").classList.add("active");
    $("#profileName").textContent = "请先登录";
    $("#profileMeta").textContent = "user:profile:{userId}";
    $("#currentUserAvatar").textContent = "?";
  }
}

// API: Refresh User profile metadata
async function refreshUserProfile() {
  try {
    const profile = await request("/user/profile");
    state.profile = profile;
    
    // Fill sidebar and profile card details
    $("#sessionUser").textContent = profile.nickname || profile.username;
    $("#profileName").textContent = profile.nickname || profile.username;
    $("#profileMeta").textContent = `user:profile:${profile.userId} (ID: ${profile.userId})`;
    
    const initials = (profile.nickname || profile.username || "?").charAt(0).toUpperCase();
    $("#currentUserAvatar").textContent = initials;
    $("#profileAvatar").textContent = initials;
    
    const inputNick = $("#newNickname");
    if (inputNick) inputNick.value = profile.nickname || "";
    
    updateMetricsUI();
  } catch (err) {
    showToast("加载个人资料失败", "error");
  }
}

// API: Get captcha code
async function fetchCaptcha() {
  const usernameInput = $("#loginUsername");
  const username = usernameInput ? usernameInput.value.trim() : "";
  if (!username) {
    showToast("请输入用户名以获取验证码", "error");
    return;
  }

  try {
    const data = await request("/auth/captcha", {
      method: "POST",
      body: JSON.stringify({ username })
    });
    
    // Autofill captcha input
    const captchaInput = $("#loginCaptcha");
    if (captchaInput) captchaInput.value = data.code;
    
    showToast("验证码获取成功！已自动填入", "success");
  } catch (err) {
    showToast(err.message || "获取验证码失败", "error");
  }
}

// API: Update user profile
async function updateProfileNickname(e) {
  e.preventDefault();
  const nickname = $("#newNickname").value.trim();
  if (!nickname) return;

  try {
    await request("/user/profile", {
      method: "PUT",
      body: JSON.stringify({ nickname })
    });
    showToast("昵称更新成功，已刷新缓存", "success");
    refreshUserProfile();
    if (state.currentTeamId) {
      refreshMembers(); // refresh current members roles/names
    }
  } catch (err) {
    showToast(err.message || "更新昵称失败", "error");
  }
}

// API: Send Presence Heartbeat
async function sendHeartbeat() {
  if (!state.currentTeamId) return;
  try {
    await request("/presence/heartbeat", {
      method: "POST",
      body: JSON.stringify({ teamId: Number(state.currentTeamId) })
    });
    
    const now = new Date();
    $("#heartbeatTime").textContent = now.toLocaleTimeString();
  } catch (err) {
    console.error("Heartbeat error:", err);
  }
}

// API: Get online presence list
async function refreshOnlineList() {
  if (!state.currentTeamId) return;
  try {
    const data = await request(`/teams/${state.currentTeamId}/online-members`);
    state.onlineMembers = data.members || [];
    
    // Render list
    const container = $("#onlineList");
    if (!container) return;
    
    container.innerHTML = "";
    if (state.onlineMembers.length === 0) {
      container.innerHTML = '<p class="text-muted" style="font-size: 12px; padding: 10px;">当前无在线成员</p>';
      return;
    }

    state.onlineMembers.forEach(m => {
      const row = document.createElement("div");
      row.className = "online-user-row";
      row.innerHTML = `
        <span class="online-user-dot"></span>
        <span style="font-size: 13px; font-weight: 500;">${m.nickname || m.username}</span>
        <span style="font-size: 10px; color: var(--text-muted); margin-left: auto;">ID: ${m.userId}</span>
      `;
      container.appendChild(row);
    });

    updateMetricsUI();
  } catch (err) {
    console.error("Error loading online presence:", err);
  }
}

// API: Create new team
async function createTeam(e) {
  e.preventDefault();
  const input = e.target.querySelector("input");
  const name = input.value.trim();
  if (!name) return;

  try {
    const data = await request("/teams", {
      method: "POST",
      body: JSON.stringify({ name })
    });
    
    showToast(`团队 "${name}" 创建成功！`, "success");
    input.value = "";
    $("#teamForm").style.display = "none";

    // Track locally
    let localTeams = JSON.parse(localStorage.getItem("teampulse_saved_teams") || "[]");
    localTeams.push({ id: data.teamId, name: name });
    localStorage.setItem("teampulse_saved_teams", JSON.stringify(localTeams));

    await refreshTeamsDropdown();
    setCurrentTeam(data.teamId);
  } catch (err) {
    showToast(err.message || "创建团队失败", "error");
  }
}

// API: Add members to team
async function addMember(e) {
  e.preventDefault();
  if (!state.currentTeamId) {
    showToast("请先选择或创建一个团队", "error");
    return;
  }

  const userIdInput = $("#memberUserId");
  const userId = Number(userIdInput.value);
  if (!userId) return;

  try {
    await request(`/teams/${state.currentTeamId}/members`, {
      method: "POST",
      body: JSON.stringify({ userId })
    });
    
    showToast(`成功添加成员 #${userId}`, "success");
    userIdInput.value = "";
    refreshMembers();
    refreshActivities();
  } catch (err) {
    showToast(err.message || "添加成员失败", "error");
  }
}

// API: Fetch team members
async function refreshMembers() {
  if (!state.currentTeamId) return;
  try {
    const data = await request(`/teams/${state.currentTeamId}/members`);
    state.members = data.members || [];
    
    const container = $("#memberList");
    if (!container) return;

    container.innerHTML = "";
    if (state.members.length === 0) {
      container.innerHTML = '<p class="text-muted" style="font-size: 13px;">团队暂无成员</p>';
      return;
    }

    state.members.forEach(m => {
      const row = document.createElement("div");
      row.className = "member-item-row";
      
      const initials = (m.nickname || m.username || "?").charAt(0).toUpperCase();
      const roleClass = m.role === "owner" ? "owner" : "member";
      const roleText = m.role === "owner" ? "所有者" : "成员";

      row.innerHTML = `
        <div class="member-avatar">${initials}</div>
        <div class="member-details">
          <div class="member-name">${m.nickname || m.username}</div>
          <div class="member-uid">用户 ID: ${m.userId}</div>
        </div>
        <span class="member-role-badge ${roleClass}">${roleText}</span>
      `;
      container.appendChild(row);
    });
  } catch (err) {
    console.error("Load members error:", err);
  }
}

// API: Load tasks list
async function refreshTasks() {
  if (!state.currentTeamId) return;
  try {
    const data = await request(`/teams/${state.currentTeamId}/tasks`);
    state.tasks = data.tasks || [];
    renderKanban();
    updateMetricsUI();
  } catch (err) {
    showToast("加载任务列表失败", "error");
  }
}

// Render tasks onto Kanban columns
function renderKanban() {
  const columns = {
    todo: $("#kanban-todo"),
    doing: $("#kanban-doing"),
    done: $("#kanban-done")
  };

  const counts = {
    todo: $("#count-todo"),
    doing: $("#count-doing"),
    done: $("#count-done")
  };

  // Reset
  Object.values(columns).forEach(col => {
    if (col) col.innerHTML = "";
  });

  const totals = { todo: 0, doing: 0, done: 0 };

  state.tasks.forEach(task => {
    const col = columns[task.status];
    if (!col) return;

    totals[task.status]++;

    const card = document.createElement("div");
    card.className = "task-card-el";
    card.draggable = true;
    card.dataset.id = task.taskId;
    
    // Drag events
    card.addEventListener("dragstart", (e) => {
      card.classList.add("dragging");
      e.dataTransfer.setData("text/plain", task.taskId);
    });

    card.addEventListener("dragend", () => {
      card.classList.remove("dragging");
    });

    // View card detail trigger
    card.addEventListener("click", (e) => {
      if (e.target.closest(".card-actions-btn")) return;
      openTaskDetail(task.taskId);
    });

    const pClass = `p-${task.priority === 3 ? 'high' : task.priority === 2 ? 'mid' : 'low'}`;
    const pText = PRIORITY_LABELS[task.priority] || "低";

    card.innerHTML = `
      <div class="task-card-header">
        <span class="task-id">#TASK-${task.taskId}</span>
        <span class="task-priority-badge ${pClass}">${pText}</span>
      </div>
      <h4>${escapeHtml(task.title)}</h4>
      <p>${escapeHtml(task.description)}</p>
      <div class="task-card-footer">
        <div class="assignee-info">
          <div class="assignee-icon"><i class="fa-solid fa-user-tie"></i></div>
          <span>负责人: ${task.assigneeId ? `ID ${task.assigneeId}` : "未分配"}</span>
        </div>
        <button class="card-actions-btn" title="查看详情">
          <i class="fa-solid fa-ellipsis"></i>
        </button>
      </div>
    `;

    col.appendChild(card);
  });

  // Set counters
  Object.keys(counts).forEach(key => {
    if (counts[key]) counts[key].textContent = totals[key];
  });
}

// API: Get single task detail & increment views
async function openTaskDetail(taskId) {
  try {
    const data = await request(`/tasks/${taskId}`);
    const task = data.task;
    state.selectedTask = task;

    // Fill drawer content
    $("#selectedTaskHeaderId").textContent = `#TASK-${task.taskId}`;
    $("#selectedTaskHeaderTitle").textContent = task.title;
    
    const statusEl = $("#selectedTaskStatus");
    statusEl.textContent = STATUS_LABELS[task.status] || task.status;
    statusEl.className = `task-status ${task.status}`;

    $("#selectedTaskMeta").textContent = `分配人ID: ${task.creatorId} | 负责人ID: ${task.assigneeId || "未分配"} | 优先级: ${PRIORITY_LABELS[task.priority]}`;

    // Edit fields
    const form = $("#taskEditForm");
    form.elements.title.value = task.title;
    form.elements.description.value = task.description;
    form.elements.assigneeId.value = task.assigneeId || "";
    form.elements.priority.value = task.priority;

    // Highlight current status switch button
    $$(".task-status-action").forEach(btn => {
      btn.classList.toggle("current", btn.dataset.status === task.status);
    });

    // Open Drawer
    $("#taskDetailDrawer").classList.add("active");

    // Automatically trigger update on hot ranking list
    refreshHotTasks(false);
  } catch (err) {
    showToast("获取任务详情失败", "error");
  }
}

// API: Update Task attributes
async function updateTask(e) {
  e.preventDefault();
  if (!state.selectedTask) return;

  const form = e.target;
  const payload = {
    title: form.elements.title.value.trim(),
    description: form.elements.description.value.trim(),
    assigneeId: Number(form.elements.assigneeId.value || 0),
    priority: Number(form.elements.priority.value)
  };

  try {
    await request(`/tasks/${state.selectedTask.taskId}`, {
      method: "PUT",
      body: JSON.stringify(payload)
    });

    showToast("任务信息更新成功", "success");
    $("#taskDetailDrawer").classList.remove("active");
    
    refreshTasks();
    refreshActivities();
    refreshHotTasks();
  } catch (err) {
    showToast(err.message || "修改任务失败", "error");
  }
}

// API: Patch Task Status
async function patchTaskStatus(taskId, status) {
  try {
    await request(`/tasks/${taskId}/status`, {
      method: "PATCH",
      body: JSON.stringify({ status })
    });
    
    showToast("任务状态流转成功", "success");
    
    // Reload data context
    refreshTasks();
    refreshActivities();
    refreshHotTasks();
    
    if (state.selectedTask && state.selectedTask.taskId == taskId) {
      openTaskDetail(taskId); // refresh drawer state
    }
  } catch (err) {
    showToast(err.message || "更新状态失败", "error");
  }
}

// API: Create new Task
async function createTask(e) {
  e.preventDefault();
  if (!state.currentTeamId) {
    showToast("请先选择或创建一个团队", "error");
    return;
  }

  const form = e.target;
  const payload = {
    title: form.elements.title.value.trim(),
    description: form.elements.description.value.trim(),
    assigneeId: Number(form.elements.assigneeId.value || 0),
    priority: Number(form.elements.priority.value)
  };

  try {
    await request(`/teams/${state.currentTeamId}/tasks`, {
      method: "POST",
      body: JSON.stringify(payload)
    });

    showToast("新任务创建成功", "success");
    form.reset();
    if (form.elements.assigneeId) form.elements.assigneeId.value = 0;
    if (form.elements.priority) form.elements.priority.value = 2;
    
    $("#newTaskModal").classList.remove("active");

    refreshTasks();
    refreshActivities();
    refreshHotTasks();
  } catch (err) {
    showToast(err.message || "创建任务失败", "error");
  }
}

// API: Load Notifications list & unread count
async function refreshNotifications(shouldLog = true) {
  if (!state.token) return;
  try {
    const [notifData, countData] = await Promise.all([
      request("/notifications"),
      request("/notifications/unread-count")
    ]);

    state.notifications = notifData.notifications || [];
    state.unreadCount = countData.count || 0;

    renderNotifications();
    updateMetricsUI();
  } catch (err) {
    console.error("Notifications fetch error:", err);
  }
}

// Render Notifications
function renderNotifications() {
  const container = $("#notificationList");
  const compactContainer = $("#notificationListCompact");

  // Render Full Page Tab notifications
  if (container) {
    container.innerHTML = "";
    if (state.notifications.length === 0) {
      container.innerHTML = '<p class="text-muted" style="padding: 20px; text-align: center;">暂无通知记录</p>';
    } else {
      state.notifications.forEach(n => {
        const item = document.createElement("div");
        item.className = `notification-row ${n.isRead === 0 ? "unread" : ""}`;
        
        let typeIcon = "fa-bell";
        if (n.type === "task_assigned") typeIcon = "fa-user-tag";
        if (n.type === "task_status_changed") typeIcon = "fa-circle-notch";

        item.innerHTML = `
          ${n.isRead === 0 ? '<div class="notif-unread-dot"></div>' : '<div style="width: 8px;"></div>'}
          <div class="metric-icon" style="width: 32px; height: 32px; font-size: 12px; background: rgba(255,255,255,0.05);">
            <i class="fa-solid ${typeIcon}"></i>
          </div>
          <div class="notif-info">
            <p>${escapeHtml(n.content)}</p>
            <small>类型: ${n.type} | 任务: #${n.relatedTaskId} | 创建时间: ${n.createdAt ? new Date(n.createdAt * 1000).toLocaleString() : "未知"}</small>
          </div>
          <div class="notif-actions">
            ${n.isRead === 0 ? `<button class="btn btn-secondary btn-small mark-read-btn" data-id="${n.notificationId}">标记已读</button>` : ""}
            <button class="btn btn-primary btn-small view-notif-task-btn" data-task-id="${n.relatedTaskId}"><i class="fa-solid fa-folder-open"></i></button>
          </div>
        `;
        container.appendChild(item);
      });
    }
  }

  // Render Sidebar Right Compact panel
  if (compactContainer) {
    compactContainer.innerHTML = "";
    const unreads = state.notifications.filter(n => n.isRead === 0);
    if (unreads.length === 0) {
      compactContainer.innerHTML = '<p class="text-muted" style="font-size: 12px;">暂无未读通知</p>';
    } else {
      unreads.slice(0, 5).forEach(n => {
        const item = document.createElement("div");
        item.className = "compact-item";
        item.innerHTML = `
          <i class="fa-solid fa-circle" style="color: var(--primary); font-size: 8px; flex-shrink: 0;"></i>
          <span style="flex: 1; overflow: hidden; text-overflow: ellipsis; white-space: nowrap;">${escapeHtml(n.content)}</span>
        `;
        item.addEventListener("click", () => openTaskDetail(n.relatedTaskId));
        compactContainer.appendChild(item);
      });
    }
  }
}

// API: Mark notification as read
async function markNotificationRead(id) {
  try {
    await request(`/notifications/${id}/read`, {
      method: "PATCH"
    });
    showToast("消息已标记为已读", "success");
    refreshNotifications(false);
  } catch (err) {
    showToast("标记已读失败", "error");
  }
}

// API: Load activities
async function refreshActivities(shouldLog = true) {
  if (!state.currentTeamId) return;
  try {
    const data = await request(`/teams/${state.currentTeamId}/activities`);
    state.activities = data.activities || [];

    const container = $("#activityList");
    if (!container) return;

    container.innerHTML = "";
    if (state.activities.length === 0) {
      container.innerHTML = '<p class="text-muted" style="padding: 20px; text-align: center;">暂无动态流记录</p>';
      return;
    }

    state.activities.forEach(act => {
      const item = document.createElement("div");
      
      let cssClass = "t-created";
      let icon = "fa-plus";
      if (act.action === "member.added") { cssClass = "m-added"; icon = "fa-user-plus"; }
      else if (act.action === "task_created") { cssClass = "task-c"; icon = "fa-file-circle-plus"; }
      else if (act.action === "task_status_updated") { cssClass = "task-u"; icon = "fa-arrows-spin"; }
      else if (act.action === "task_updated") { cssClass = "task-u"; icon = "fa-pen-to-square"; }

      item.className = `timeline-item ${cssClass}`;
      item.innerHTML = `
        <div class="timeline-icon"><i class="fa-solid ${icon}"></i></div>
        <div class="timeline-time">${new Date(act.createdAt * 1000).toLocaleString()}</div>
        <div class="timeline-content">
          <strong>${escapeHtml(act.content)}</strong>
          <div style="font-size: 11px; color: var(--text-muted); margin-top: 2px;">
            操作员 ID: ${act.actorId} ${act.targetUserId ? `| 目标用户 ID: ${act.targetUserId}` : ""}
          </div>
        </div>
      `;
      container.appendChild(item);
    });
  } catch (err) {
    console.error("Load activities error:", err);
  }
}

// API: Load hot tasks排行
async function refreshHotTasks(shouldLog = true) {
  if (!state.currentTeamId) return;
  try {
    const data = await request(`/teams/${state.currentTeamId}/tasks/hot`);
    state.hotTasks = data.tasks || [];

    const container = $("#hotTaskList");
    if (!container) return;

    container.innerHTML = "";
    if (state.hotTasks.length === 0) {
      container.innerHTML = '<p class="text-muted" style="padding: 10px; font-size: 12px;">暂无热门任务，打开任务详情即可增加热度分</p>';
      return;
    }

    state.hotTasks.forEach((task, index) => {
      const row = document.createElement("div");
      row.className = "hot-task-row";
      row.innerHTML = `
        <div class="hot-rank">#${index + 1}</div>
        <div class="hot-info">
          <h5>${escapeHtml(task.title)}</h5>
          <span>状态: ${STATUS_LABELS[task.status] || task.status}</span>
        </div>
        <div class="hot-score">
          <i class="fa-solid fa-fire"></i> ${task.viewCount}
        </div>
      `;
      row.addEventListener("click", () => openTaskDetail(task.taskId));
      container.appendChild(row);
    });
  } catch (err) {
    console.error("Load hot tasks error:", err);
  }
}

// Update top statistics metrics
function updateMetricsUI() {
  $("#taskMetric").textContent = state.tasks.filter(t => t.assigneeId === state.profile?.userId).length;
  $("#presenceMetric").textContent = state.onlineMembers.length;
  $("#notificationMetric").textContent = state.unreadCount;
  $("#hotMetric").textContent = state.hotTasks.length;
  
  // Sidebar unread count badge
  const navBadge = $("#navUnreadBadge");
  if (navBadge) {
    navBadge.textContent = state.unreadCount;
    navBadge.style.display = state.unreadCount > 0 ? "block" : "none";
  }

  const rightBadge = $("#unreadBadge");
  if (rightBadge) {
    rightBadge.textContent = state.unreadCount;
  }
}

// Helper to escape HTML tags to avoid XSS injections
function escapeHtml(unsafe) {
  if (!unsafe) return "";
  return unsafe
    .replace(/&/g, "&amp;")
    .replace(/</g, "&lt;")
    .replace(/>/g, "&gt;")
    .replace(/"/g, "&quot;")
    .replace(/'/g, "&#039;");
}

// Event Bindings setup on window load
function initEvents() {
  // Login & Register Tab Navigation
  $$(".auth-tab").forEach(tab => {
    tab.addEventListener("click", () => {
      $$(".auth-tab").forEach(t => t.classList.remove("active"));
      $$(".auth-form").forEach(f => f.classList.remove("active"));
      
      tab.classList.add("active");
      $(`[data-pane="${tab.dataset.tab}"]`).classList.add("active");
    });
  });

  // Get Captcha autofill
  const capBtn = $("#captchaBtn");
  if (capBtn) capBtn.addEventListener("click", fetchCaptcha);

  // Authentication forms submission
  const loginForm = $("#loginForm");
  if (loginForm) {
    loginForm.addEventListener("submit", async (e) => {
      e.preventDefault();
      const username = loginForm.elements.username.value.trim();
      const password = loginForm.elements.password.value;
      const captcha = loginForm.elements.captcha.value.trim();

      try {
        const res = await request("/auth/login", {
          method: "POST",
          body: JSON.stringify({ username, password, captcha })
        });
        showToast("登录成功！", "success");
        setToken(res.token);
        loginForm.reset();
      } catch (err) {
        showToast(err.message || "登录失败，请检查验证码或账号密码", "error");
      }
    });
  }

  const registerForm = $("#registerForm");
  if (registerForm) {
    registerForm.addEventListener("submit", async (e) => {
      e.preventDefault();
      const username = registerForm.elements.username.value.trim();
      const password = registerForm.elements.password.value;
      const nickname = registerForm.elements.nickname.value.trim();

      try {
        await request("/auth/register", {
          method: "POST",
          body: JSON.stringify({ username, password, nickname })
        });
        showToast("注册成功！请切换到登录页面登录", "success");
        registerForm.reset();
        // Shift to login pane
        $("[data-tab='login']").click();
      } catch (err) {
        showToast(err.message || "注册失败，用户名可能已存在", "error");
      }
    });
  }

  // Logout button
  const logoutBtn = $("#logoutBtn");
  if (logoutBtn) {
    logoutBtn.addEventListener("click", async () => {
      try {
        await request("/auth/logout", { method: "POST" });
        showToast("已成功退出登录", "success");
      } catch (err) {
        console.error("Logout query status:", err);
      } finally {
        logout();
      }
    });
  }

  // Edit profile nickname
  const profileForm = $("#profileForm");
  if (profileForm) profileForm.addEventListener("submit", updateProfileNickname);

  // Team Selector dropdown
  const selector = $("#teamSelector");
  if (selector) {
    selector.addEventListener("change", (e) => {
      setCurrentTeam(e.target.value);
    });
  }

  // Show/Hide New team form inline
  const btnAddTeam = $("#toggleNewTeamForm");
  if (btnAddTeam) {
    btnAddTeam.addEventListener("click", () => {
      const f = $("#teamForm");
      f.style.display = f.style.display === "none" ? "block" : "none";
    });
  }

  // Create team form
  const teamForm = $("#teamForm");
  if (teamForm) teamForm.addEventListener("submit", createTeam);

  // Add member form
  const memberForm = $("#memberForm");
  if (memberForm) memberForm.addEventListener("submit", addMember);

  // Refresh buttons
  const btnMembers = $("#loadMembersBtn");
  if (btnMembers) btnMembers.addEventListener("click", refreshMembers);
  
  const btnNotifs = $("#loadNotificationsBtn");
  if (btnNotifs) btnNotifs.addEventListener("click", () => refreshNotifications(true));
  
  const btnActs = $("#loadActivitiesBtn");
  if (btnActs) btnActs.addEventListener("click", () => refreshActivities(true));
  
  const btnHot = $("#loadHotTasksBtn");
  if (btnHot) btnHot.addEventListener("click", () => refreshHotTasks(true));

  const btnOnline = $("#loadOnlineBtn");
  if (btnOnline) btnOnline.addEventListener("click", refreshOnlineList);

  // Clear log debugger console
  const clearLogBtn = $("#clearLogBtn");
  if (clearLogBtn) {
    clearLogBtn.addEventListener("click", () => {
      const output = $("#logOutput");
      if (output) output.textContent = "等待网络操作...";
    });
  }

  // Drawer details controls
  const closeDrawerBtn = $("#closeDrawerBtn");
  if (closeDrawerBtn) {
    closeDrawerBtn.addEventListener("click", () => {
      $("#taskDetailDrawer").classList.remove("active");
    });
  }

  const editTaskForm = $("#taskEditForm");
  if (editTaskForm) editTaskForm.addEventListener("submit", updateTask);

  // Drawer actions for status shift buttons
  $$(".task-status-action").forEach(btn => {
    btn.addEventListener("click", () => {
      if (!state.selectedTask) return;
      const nextStatus = btn.dataset.status;
      patchTaskStatus(state.selectedTask.taskId, nextStatus);
    });
  });

  // Create task modal dialog controls
  const modalTask = $("#newTaskModal");
  const openNewTaskBtn = $("#openNewTaskModal");
  const quickCreateTaskBtn = $("#quickCreateTaskBtn");
  const closeNewTaskModal = $("#closeNewTaskModal");
  const cancelNewTask = $("#cancelNewTask");
  const taskCreateForm = $("#taskCreateForm");

  const openTaskModal = () => {
    if (!state.currentTeamId) {
      showToast("请先选择或创建一个工作团队", "error");
      return;
    }
    modalTask.classList.add("active");
  };

  if (openNewTaskBtn) openNewTaskBtn.addEventListener("click", openTaskModal);
  if (quickCreateTaskBtn) quickCreateTaskBtn.addEventListener("click", openTaskModal);

  const hideModal = () => modalTask.classList.remove("active");
  if (closeNewTaskModal) closeNewTaskModal.addEventListener("click", hideModal);
  if (cancelNewTask) cancelNewTask.addEventListener("click", hideModal);
  if (taskCreateForm) taskCreateForm.addEventListener("submit", createTask);

  // Mark unread notification read & open task in drawer from notification click links
  const notifContainer = $("#notificationList");
  if (notifContainer) {
    notifContainer.addEventListener("click", (e) => {
      const readBtn = e.target.closest(".mark-read-btn");
      if (readBtn) {
        markNotificationRead(readBtn.dataset.id);
        return;
      }
      
      const viewBtn = e.target.closest(".view-notif-task-btn");
      if (viewBtn) {
        const tid = Number(viewBtn.dataset.taskId);
        if (tid) openTaskDetail(tid);
      }
    });
  }

  // Sidebar link Navigation tab pane switcher
  $$(".nav-item").forEach(item => {
    item.addEventListener("click", () => {
      $$(".nav-item").forEach(i => i.classList.remove("active"));
      $$(".view-pane").forEach(p => p.classList.remove("active"));

      item.classList.add("active");
      const targetId = `view-${item.dataset.target}`;
      const targetPane = $(`#${targetId}`);
      if (targetPane) targetPane.classList.add("active");

      // Collapsible mobile sidebar cleanup
      const sidebar = $("#sidebarMenu");
      if (sidebar) sidebar.classList.remove("active");
    });
  });

  // Mobile menu Hamburger toggle
  const mobileToggle = $("#mobileMenuToggle");
  if (mobileToggle) {
    mobileToggle.addEventListener("click", () => {
      const sidebar = $("#sidebarMenu");
      if (sidebar) sidebar.classList.toggle("active");
    });
  }

  // Kanban Native Drag and Drop columns dropzones
  $$(".kanban-column").forEach(col => {
    col.addEventListener("dragover", (e) => {
      e.preventDefault(); // crucial for drop to trigger
      col.classList.add("drag-over");
    });

    col.addEventListener("dragenter", (e) => {
      e.preventDefault();
      col.classList.add("drag-over");
    });

    col.addEventListener("dragleave", () => {
      col.classList.remove("drag-over");
    });

    col.addEventListener("drop", (e) => {
      col.classList.remove("drag-over");
      const taskId = e.dataTransfer.getData("text/plain");
      const targetStatus = col.dataset.status;
      if (taskId && targetStatus) {
        patchTaskStatus(Number(taskId), targetStatus);
      }
    });
  });
}

// Initial script bootstrap
window.addEventListener("DOMContentLoaded", () => {
  initEvents();
  updateUIOnAuthState();
});
