"use strict";
// token 从本页 URL 提取一次（面板 URL 即凭据；不写 localStorage 不外发）。
const TOKEN = new URLSearchParams(location.search).get("token") || "";
const H = { "Authorization": "Bearer " + TOKEN };
// 首跑配置模式（URL setup=1）：隐藏运行期动作/诊断卡与历史账号切换，
// 只留状态标题 + 设置表单——这些端点未接线时点了只会报 501，徒增困惑。
const SETUP = new URLSearchParams(location.search).get("setup") === "1";

// 状态图标用固定形状词汇 + DOM 构建（不用 innerHTML，见 setGlyph）。// 颜色/文案唯一出处 = 回路 /state 快照的 color/label（rule #6，不在此重抄词表）。
// 形状不在契约里，留本地表；空闲色从 CSS 变量取（DESIGN.md 链路：md→:root→JS）。
const IDLE = getComputedStyle(document.documentElement).getPropertyValue("--idle").trim();
const SHAPES = { online:"solid", offline:"ring", portal_down:"ring", rejected:"ring_slash" };

const SVG_NS = "http://www.w3.org/2000/svg";

function svgEl(name, attrs) {
  const el = document.createElementNS(SVG_NS, name);
  for (const [k, v] of Object.entries(attrs)) el.setAttribute(k, v);
  return el;
}

function setGlyph(color, state) {
  // currentColor 读 CSS color 属性（不是 fill 属性）——设在 svg 上，子元素 currentColor 才生效。
  document.querySelector(".state-icon").style.color = color || IDLE;
  const g = document.getElementById("glyph");
  g.replaceChildren();
  const shape = SHAPES[state] || "lamp";
  if (shape === "solid") {
    g.appendChild(svgEl("circle", { cx: 16, cy: 16, r: 12, fill: "currentColor" }));
    return;
  }
  g.appendChild(svgEl("circle", {
    cx: 16, cy: 16, r: 10, fill: "none", stroke: "currentColor", "stroke-width": 4,
  }));
  if (shape === "ring_slash") {
    g.appendChild(svgEl("line", {
      x1: 8, y1: 24, x2: 24, y2: 8, stroke: "currentColor", "stroke-width": 4,
    }));
  } else if (shape === "lamp") {
    g.appendChild(svgEl("circle", { cx: 16, cy: 16, r: 5, fill: "currentColor" }));
  }
}

async function refresh() {
  try {
    const r = await fetch("/state", { headers: H });
    if (!r.ok) throw new Error(r.status);
    const s = await r.json();
    // state="" 是首拍前的空档（词表对未知状态回退状态名本身→空串）：
    // 大卡不得显示空白，回落「启动中」。
    const label = s.label || s.state || "启动中";
    document.getElementById("label").textContent = label;
    document.getElementById("detail").textContent = s.detail || "";
    renderReadings(s);
    setGlyph(s.color, s.state);
    document.getElementById("v4").textContent = s.v4_src || "未绑定";
    document.getElementById("v6").textContent = s.v6_src || "无 IPv6 源";
    document.getElementById("route").textContent = s.route || "尚未选路";
    document.getElementById("sess").textContent = s.extra && s.extra.session_dead ? "已判死（重连中）" : "正常";
    document.getElementById("free").textContent = s.extra && s.extra.free_mode ? "已进入（自动检测）" : "未触发";
    // 只有门户明确观察「未在线」时才给补绑动作——其余态显示它等于无意义按钮。
    document.getElementById("rebind-btn").hidden = s.v6_state !== "offline";
    renderFlux(s.flux);
    const t = new Date();
    document.getElementById("meta").textContent =
      "更新于 " + t.toLocaleTimeString("zh-CN", { hour12: false });
  } catch (e) {
    document.getElementById("label").textContent = "守护无响应";
    document.getElementById("detail").textContent = "回路 API 不可达（" + e.message + "）";
    setGlyph("", "paused");
  }
}

// 双读数（2026-11-18）：认证（物理栈）+ 用户路径（系统栈）+ 原因一句话。
// 字段缺失（旧内核）时整行留空，不编造。
function renderReadings(s) {
  const el = document.getElementById("readings");
  if (!el) return;
  const auth = { online: "认证在线", offline: "认证离线", unknown: "认证未知" }[s.auth_state] || "";
  const path = { ok: "用户路径通", broken: "用户路径不通", unknown: "用户路径未知" }[s.user_path] || "";
  const parts = [];
  if (auth) parts.push(auth);
  if (path) parts.push(path);
  if (s.reason_label) parts.push(s.reason_label);
  el.textContent = parts.join("｜");
}

// 今日流量：从 /state 的 flux 快照常驻渲染（内核 10 分钟低频轮询，不额外
// 打门户）。算不出百分比时如实留空——原文照列，绝不编造数字。
function renderFlux(f) {
  const used = document.getElementById("flux-used");
  const quota = document.getElementById("flux-quota");
  const percent = document.getElementById("flux-percent");
  const bar = document.getElementById("flux-bar");
  const track = document.getElementById("flux-track");
  if (!f) {
    used.textContent = "未查询";
    quota.textContent = "";
    percent.textContent = "—";
    bar.style.width = "0";
    bar.removeAttribute("data-level");
    track.setAttribute("aria-valuenow", "0");
    return;
  }
  used.textContent = f.used || "已用未知";
  quota.textContent = f.quota ? "/ " + f.quota : "";
  if (f.has_percent) {
    const pct = Math.max(0, Math.min(100, Math.round(f.percent)));
    percent.textContent = pct + "%";
    bar.style.width = pct + "%";
    track.setAttribute("aria-valuenow", String(pct));
  } else {
    percent.textContent = "—";
    bar.style.width = "0";
    track.setAttribute("aria-valuenow", "0");
  }
  if (f.level === "warn" || f.level === "critical") {
    bar.setAttribute("data-level", f.level);
  } else {
    bar.removeAttribute("data-level");
  }
}

document.getElementById("login").addEventListener("click", async (ev) => {
  const b = ev.currentTarget;
  b.disabled = true;
  b.textContent = "检测中…";
  try {
    const r = await fetch("/login", { method: "POST", headers: H });
    if (!r.ok) {
      const msg = await r.text();
      document.getElementById("detail").textContent = "手动重连：" + msg;
    }
  } finally {
    b.disabled = false;
    b.textContent = "立即检测重连";
    refresh();
  }
});

// 查流量：手动触发（POST userflux 按源 IP 认会话，频繁轮询既撞限频又刷日志）。
// 失败把门户原因写进行内——「错误」键与 fetch 失败都显示原文，不造假数据。
document.getElementById("flux-btn").addEventListener("click", async (ev) => {
  const b = ev.currentTarget;
  const el = document.getElementById("flux");
  b.disabled = true;
  b.textContent = "查询中…";
  el.textContent = "查询中…";
  try {
    const r = await fetch("/flux", { headers: H });
    const data = await r.json().catch(() => null);
    if (r.ok && data) {
      // 键名是校事实（参考校：当天流量/日流量额；他校可能：已用流量/剩余流量）。
      // 常见键按优先级展示；全都没有就展示其它字段——绝不把「用户名称」当流量。
      const preferred = ["当天流量", "日流量额", "已用流量", "剩余流量"];
      const parts = preferred.filter((k) => data[k]).map((k) => k + " " + data[k]);
      const others = Object.entries(data)
        .filter(([k, v]) => v && k !== "用户名称" && !preferred.includes(k));
      el.textContent = parts.length ? parts.join(" / ")
        : others.length ? others.map(([k, v]) => k + " " + v).join("，")
        : "已查询（门户未返回流量字段）";
      el.title = "查询于 " + new Date().toLocaleTimeString("zh-CN", { hour12: false }) + "\n" +
        Object.entries(data).map(([k, v]) => k + "：" + v).join("\n");
    } else {
      el.textContent = "查询失败：" + (data && data["错误"] ? data["错误"] : (await r.text() || r.status));
      el.title = "";
    }
  } catch (e) {
    el.textContent = "查询失败：" + e.message;
    el.title = "";
  } finally {
    b.disabled = false;
    b.textContent = "查流量明细";
  }
});

// v6 补绑：仅在 v6_state=offline（门户观察未登记）时可用。成功后 v6 观察有
// 60s 节流，提示用户稍后自动刷新，不假报「已在线」。
document.getElementById("rebind-btn").addEventListener("click", async (ev) => {
  const b = ev.currentTarget;
  b.disabled = true;
  b.textContent = "补绑中…";
  try {
    const r = await fetch("/rebindv6", { method: "POST", headers: H });
    const msg = await r.text();
    document.getElementById("v6").textContent = r.ok
      ? "已提交补绑（观察有 60s 节流，稍后自动刷新）"
      : "补绑失败：" + msg;
  } catch (e) {
    document.getElementById("v6").textContent = "补绑失败：" + e.message;
  } finally {
    b.disabled = false;
    b.textContent = "补绑 IPv6";
    refresh();
  }
});

// 可视化日志：hidden 卡按需展开（托盘「查看日志」带 logs=1 直达）。
// 只拉尾部 200 行；空文件显示提示而不是空白框。
async function loadLogs() {
  const card = document.getElementById("log-card");
  const view = document.getElementById("log-view");
  card.hidden = false;
  try {
    const r = await fetch("/logs", { headers: H });
    if (!r.ok) {
      view.textContent = "日志读取失败：" + (await r.text());
      return;
    }
    const lines = await r.json();
    view.textContent = lines && lines.length ? lines.join("\n") : "暂无日志（状态变化时自动记录）";
    view.scrollTop = view.scrollHeight; // 新的在最后，默认看最新
  } catch (e) {
    view.textContent = "日志读取失败：" + e.message;
  }
}

document.getElementById("log-btn").addEventListener("click", () => loadLogs());
document.getElementById("log-refresh").addEventListener("click", () => loadLogs());

// URL 带 logs=1（托盘「查看日志」入口）：页面加载即展开并滚动到日志。
if (new URLSearchParams(location.search).get("logs") === "1") {
  loadLogs().then(() => document.getElementById("log-card").scrollIntoView({ block: "end" }));
}

// URL 带 flux=1（托盘「查流量」入口）：页面加载即自动查询一次。
if (new URLSearchParams(location.search).get("flux") === "1") {
  document.getElementById("flux-btn").click();
}
