// 设置侧脚本：与 panel.js（状态侧）按序拼接后进同一个 <script>（panel.go 的
// panelJS）。拆开只为过提交钩子的 20KB 上限，不是模块边界——两侧共享顶层
// 作用域与 token/H，"use strict" 只在 panel.js 出现一次。

// 免认证时段：结构化时间编辑器（type=time），多段可增删。保存前校验，
// 非法值不提交；服务端 ValidateFreeWindows 还会再拒一次（双闸）。
// configLoaded=false（配置没读到）时保存不提交 free_windows——避免把
// 读不到的现有窗口当成空值写掉。
let configLoaded = false;

function addFreeRow(start, end) {
  const row = document.createElement("div");
  row.className = "free-row";
  row.innerHTML =
    '<input type="time" class="free-start" aria-label="免认证开始时间">' +
    '<span>至</span>' +
    '<input type="time" class="free-end" aria-label="免认证结束时间">' +
    '<button type="button" class="free-del" aria-label="删除该时段">删除</button>';
  row.querySelector(".free-start").value = start || "";
  row.querySelector(".free-end").value = end || "";
  row.querySelector(".free-del").addEventListener("click", () => row.remove());
  document.getElementById("free-list").appendChild(row);
}

function renderFreeWindows(value) {
  const list = document.getElementById("free-list");
  list.innerHTML = "";
  const ranges = (value || "").split(",").map((s) => s.trim()).filter(Boolean);
  for (const r of ranges) {
    const [a, b] = r.split("-");
    // 24:00 与 23:59 在分钟判定上等价（1440 永不出现），time 输入不接受 24:00。
    addFreeRow(a, b === "24:00" ? "23:59" : b);
  }
  if (!ranges.length) addFreeRow("", ""); // 空配置给一行空编辑器；空行保存=禁用
}

function collectFreeWindows() {
  const rows = [...document.querySelectorAll("#free-list .free-row")];
  const out = [];
  for (const row of rows) {
    const a = row.querySelector(".free-start").value;
    const b = row.querySelector(".free-end").value;
    if (!a && !b) continue; // 空行忽略
    if (!a || !b) throw new Error("免认证时段：开始和结束都要填");
    if (a === b) throw new Error("免认证时段：开始和结束不能相同");
    out.push(a + "-" + b);
  }
  return out.join(",");
}

document.getElementById("free-add").addEventListener("click", () => addFreeRow("", ""));

// 高级设置：键名即 /config 契约键，视图与提交共用同一张表（不抄两份）。
const ADVANCED_FIELDS = {
  "campus_dns": "f-campus-dns", "interval": "f-interval",
  "probe_timeout": "f-probe-timeout", "login_timeout": "f-login-timeout",
  "probe_ips": "f-probe-ips", "login_page": "f-login-page",
  "user_agent": "f-user-agent", "portal_down_wait": "f-portal-down-wait",
  "reject_backoff": "f-reject-backoff", "transient_backoff": "f-transient-backoff",
  "ssl_verify": "f-ssl-verify", "success_marks": "f-success-marks",
  "negative_marks": "f-negative-marks", "transient_marks": "f-transient-marks",
  "freeze_marks": "f-freeze-marks", "login_form": "f-login-form",
};

// advancedConfigView：GET /config 的非敏感高级视图回填折叠区。
function advancedConfigView(c) {
  for (const [key, id] of Object.entries(ADVANCED_FIELDS)) {
    const el = document.getElementById(id);
    if (!el) continue;
    if (el.type === "checkbox") el.checked = c[key] === "true";
    else el.value = c[key] || "";
  }
}

// collectAdvanced：提交前收集（只在配置读成功时提交，防把读不到的值写空）。
function collectAdvanced() {
  const out = {};
  for (const [key, id] of Object.entries(ADVANCED_FIELDS)) {
    const el = document.getElementById(id);
    if (!el) continue;
    out[key] = el.type === "checkbox" ? String(el.checked) : el.value.trim();
  }
  return out;
}

// 设置区：读当前配置（密码不回传），保存成对提交。
async function loadConfig() {
  try {
    const r = await fetch("/config", { headers: H });
    if (!r.ok) return;
    const c = await r.json();
    document.getElementById("f-user").value = c.username || "";
    document.getElementById("f-server").value = c.server || "";
    renderFreeWindows(c.free_windows || "");
    document.getElementById("f-bindip").value = c.bind_ip || "";
    document.getElementById("f-auto").checked = c.auto_reconnect !== "false";
    advancedConfigView(c);
    document.getElementById("free-add").disabled = false;
    configLoaded = true;
  } catch (e) {
    // 面板只读降级：配置没读到，免认证时段编辑器不开放（保存时也不提交
    // free_windows，绝不把读不到的现有窗口写成空值）。
    document.getElementById("free-add").disabled = true;
  }
  loadAccounts();
}
// 视图切换：状态（默认，5s 轮询）与设置（首次打开才读配置/账号）。
// 设置页懒加载：只看状态时不多打 /config 与 /accounts。
const VIEWS = {
  status: { btn: document.getElementById("tab-status"), panel: document.getElementById("view-status") },
  settings: { btn: document.getElementById("tab-settings"), panel: document.getElementById("view-settings") },
};
let currentView = "status";
let pollTimer = null;

// 只在「状态页可见且窗口未隐藏」时轮询：最小化/切到设置页即停，省 CPU 与唤醒。
function syncPolling() {
  const shouldPoll = currentView === "status" && !document.hidden;
  if (shouldPoll && pollTimer === null) {
    pollTimer = setInterval(refresh, 5000);
  } else if (!shouldPoll && pollTimer !== null) {
    clearInterval(pollTimer);
    pollTimer = null;
  }
}

function showView(name) {
  currentView = name;
  for (const [key, v] of Object.entries(VIEWS)) {
    v.panel.hidden = key !== name;
    v.btn.setAttribute("aria-selected", String(key === name));
    v.btn.tabIndex = key === name ? 0 : -1; // roving tabindex（tablist 键盘模式）
  }
  if (name === "settings" && !configLoaded) loadConfig();
  if (name === "status") refresh();
  syncPolling();
}

VIEWS.status.btn.addEventListener("click", () => showView("status"));
VIEWS.settings.btn.addEventListener("click", () => showView("settings"));
// 首屏「切账号」捷径：只做导航 + 聚焦，账号列表唯一出处仍是 /accounts
//（settings 页懒加载时填充）。首次打开时下拉还在加载/disabled（禁用元素
// 拿不到焦点），等它可用再聚焦（最多 1s，空账号列表就保持现状）。
document.getElementById("acct-shortcut").addEventListener("click", async () => {
  showView("settings");
  const sel = document.getElementById("f-account");
  for (let i = 0; i < 20 && sel.disabled; i++) {
    await new Promise((r) => setTimeout(r, 50));
  }
  document.getElementById("f-account").focus();
});
// tablist 键盘模式：左右箭头在状态/设置间切换（Enter/Space 原生触发 click）。
document.getElementById("tabs").addEventListener("keydown", (e) => {
  if (e.key !== "ArrowLeft" && e.key !== "ArrowRight") return;
  const order = ["status", "settings"];
  const step = e.key === "ArrowRight" ? 1 : order.length - 1;
  const next = order[(order.indexOf(currentView) + step) % order.length];
  showView(next);
  VIEWS[next].btn.focus();
});
document.addEventListener("visibilitychange", () => {
  syncPolling();
  if (!document.hidden && currentView === "status") refresh();
});

if (SETUP) {
  // 首跑：tabs 藏掉（无处可切），但状态卡保留——setup.go 的 Snapshot
  // 「首次配置」引导文案是用户唯一看得见的说明（P2 审查：整页藏掉让
  // 状态缝变死接线）。设置区只收账号/密码/门户。
  document.getElementById("tabs").hidden = true;
  document.getElementById("actions-card").hidden = true;
  document.getElementById("diag-card").hidden = true;
  document.getElementById("log-card").hidden = true;
  document.querySelector("#f-account").closest("label").hidden = true;
  document.getElementById("switch").closest(".actions").hidden = true;
  document.getElementById("advanced").hidden = true; // 首跑只收账号与门户
  document.getElementById("save").textContent = "保存并完成配置";
  // 首跑布局：状态卡（引导文案）+ 设置卡同屏——两个视图层都显示。
  VIEWS.status.panel.hidden = false;
  VIEWS.settings.panel.hidden = false;
  currentView = "settings";
  loadConfig();
} else {
  showView("status");
}

// 历史账号下拉：GET /accounts 填充，current 预选。
async function loadAccounts() {
  const sel = document.getElementById("f-account");
  try {
    const r = await fetch("/accounts", { headers: H });
    if (!r.ok) { sel.disabled = true; return; }
    const accs = await r.json();
    sel.innerHTML = "";
    for (const a of accs) {
      const opt = document.createElement("option");
      opt.value = a.username;
      opt.textContent = a.username + (a.server ? "（" + a.server + "）" : "") + (a.current ? " · 当前" : "");
      if (a.current) opt.selected = true;
      sel.appendChild(opt);
    }
    sel.disabled = accs.length === 0;
  } catch (e) { sel.disabled = true; }
}

// 切换账号：POST /accounts {username}（密码从 keyring 还原，不重输）。
document.getElementById("switch").addEventListener("click", async (ev) => {
  const b = ev.currentTarget;
  const msg = document.getElementById("cfg-msg");
  const user = document.getElementById("f-account").value;
  if (!user) return;
  b.disabled = true;
  try {
    const r = await fetch("/accounts", {
      method: "POST", headers: { ...H, "Content-Type": "application/json" },
      body: JSON.stringify({ username: user }),
    });
    if (r.ok) {
      // 服务端回的是完整结果文案：「已切换到 X」与「认证未成功/未携带
      // ipv4or6」是两件事，面板不自造「即时生效」这种话（2026-11-09 事故：
      // 这话在 v6 上从来不是真的）。
      let text = "已切换到 " + user;
      try {
        const body = await r.json();
        if (body && body.message) text = body.message;
      } catch (e) { /* 非 JSON 就退回默认文案 */ }
      msg.textContent = text;
      loadConfig();
    } else {
      msg.textContent = "切换失败：" + (await r.text());
    }
  } catch (e) {
    msg.textContent = "切换失败：" + e.message;
  } finally {
    b.disabled = false;
  }
});

document.getElementById("save").addEventListener("click", async (ev) => {
  const b = ev.currentTarget;
  const msg = document.getElementById("cfg-msg");
  b.disabled = true;
  b.textContent = "保存中…";
  try {
    // 配置没读到时禁止整单保存：server/bind_ip/auto_reconnect 的表单是空的，
    // 原样提交会把空 server 与「自动重连关」写进 ini（P1 审查：防呆只挡
    // free_windows/高级键挡了一半）。SETUP 例外——首跑本来就从空开始。
    if (!configLoaded && !SETUP) {
      msg.textContent = "保存失败：配置未读取（点「设置」重试；仍失败请重启守护）";
      return;
    }
    const body = {
      server: document.getElementById("f-server").value.trim(),
      bind_ip: document.getElementById("f-bindip").value.trim(),
      auto_reconnect: document.getElementById("f-auto").checked ? "true" : "false",
    };
    if (configLoaded) {
      try {
        body.free_windows = collectFreeWindows();
      } catch (e) {
        msg.textContent = e.message;
        return; // finally 恢复按钮
      }
      if (!SETUP) Object.assign(body, collectAdvanced());
    }
    const pass = document.getElementById("f-pass").value;
    if (pass) { // 账号与密码成对才提交凭据（留空 = 不动凭据，只改其余设置）
      body.username = document.getElementById("f-user").value.trim();
      body.password = pass;
    }
    const r = await fetch("/config", {
      method: "POST", headers: { ...H, "Content-Type": "application/json" },
      body: JSON.stringify(body),
    });
    if (r.ok) {
      if (SETUP) {
        msg.textContent = "已保存。关闭本窗口后开始守护。";
      } else if (body.login_form) {
        // login_form 与 Marks 同纪律：启动前赋值，登录路径只读——保存只写
        // ini，下次重启生效（面板回显的是 ini 新值，别让用户误以为热生效）。
        msg.textContent = "已保存（登录表单重启后生效；其余即时或重启生效）";
      } else {
        msg.textContent = "已保存（自动重连与免认证时段即时生效；门户/网卡重启生效）";
      }
      document.getElementById("f-pass").value = "";
      refresh();
      loadConfig();
    } else {
      msg.textContent = "保存失败：" + (await r.text());
    }
  } catch (e) {
    msg.textContent = "保存失败：" + e.message;
  } finally {
    b.disabled = false;
    b.textContent = "保存设置";
  }
});
