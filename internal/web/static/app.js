// optimus web dashboard — vanilla JS, no build step.
"use strict";

const $ = (s, el = document) => el.querySelector(s);
const $$ = (s, el = document) => [...el.querySelectorAll(s)];
const ESC = { "&": "&amp;", "<": "&lt;", ">": "&gt;", '"': "&quot;", "'": "&#39;" };
// Every string that came from a session, a path or the server goes through esc().
const esc = (v) => String(v ?? "").replace(/[&<>"']/g, (c) => ESC[c]);

const S = {
  view: "fleet",
  state: null,
  selected: localStorage.getItem("optimus.win") || "",
  newAgent: "",
  sessions: [],
  transcriptOf: null,
  handoffOf: null,
};

// ---------------------------------------------------------------- api

async function api(path, opts = {}) {
  const init = { method: opts.method || "GET", headers: {} };
  if (opts.body !== undefined) {
    init.headers["Content-Type"] = "application/json";
    init.body = JSON.stringify(opts.body);
  }
  const res = await fetch(path, init);
  if (res.status === 401) {
    showLogin();
    throw new Error("not connected");
  }
  const data = await res.json().catch(() => ({}));
  if (!res.ok) throw new Error(data.error || res.statusText);
  return data;
}

function toast(msg, err = false) {
  const t = document.createElement("div");
  t.className = "toast" + (err ? " err" : "");
  t.textContent = msg;
  $("#toasts").append(t);
  setTimeout(() => t.remove(), err ? 7000 : 4000);
}

async function act(fn, okMsg) {
  try {
    const r = await fn();
    if (okMsg) toast(typeof okMsg === "function" ? okMsg(r) : okMsg);
    refresh();
    return r;
  } catch (e) {
    if (e.message !== "not connected") toast(e.message, true);
  }
}

// ---------------------------------------------------------------- formatting

const money = (v) => (!v ? "$0" : v < 0.01 ? "<$0.01" : v < 1000 ? "$" + v.toFixed(2) : "$" + Math.round(v).toLocaleString());
function tokens(n) {
  if (n < 1e3) return String(n);
  if (n < 1e6) return (n / 1e3).toFixed(1) + "k";
  if (n < 1e9) return (n / 1e6).toFixed(1) + "M";
  return (n / 1e9).toFixed(2) + "B";
}
function dur(ms) {
  const s = Math.abs(ms) / 1000;
  if (s < 60) return Math.round(s) + "s";
  if (s < 3600) return Math.round(s / 60) + "m";
  if (s < 172800) { const h = Math.floor(s / 3600), m = Math.round((s % 3600) / 60); return h < 10 && m ? `${h}h${String(m).padStart(2, "0")}m` : h + "h"; }
  return Math.round(s / 86400) + "d";
}
const ago = (t) => (!t || t.startsWith("0001") ? "–" : dur(Date.now() - new Date(t)));
const until = (t) => dur(new Date(t) - Date.now());
const short = (p) => (S.state && p && p.startsWith(S.state.home) ? "~" + p.slice(S.state.home.length) : p || "");
const level = (f) => (f >= 0.9 ? "bad" : f >= 0.6 ? "warn" : "");
const agentChip = (a) => `<span class="chip ag-${esc(a)}">${esc(a)}</span>`;
const stateBadge = (s) => `<span class="state ${esc(s)}">${{ busy: "● busy", input: "◆ input", idle: "○ idle", exited: "✗ exited" }[s] || esc(s)}</span>`;

// ---------------------------------------------------------------- views

function setView(v) {
  S.view = v;
  $$(".view").forEach((el) => el.classList.toggle("on", el.id === "view-" + v));
  $$("[data-view]").forEach((b) => b.classList.toggle("on", b.dataset.view === v));
  if (v === "sessions") loadSessions();
  if (v === "usage") loadUsage();
  if (v === "fleet") setTimeout(fit, 0);
}
$$("[data-view]").forEach((b) => b.addEventListener("click", () => setView(b.dataset.view)));

async function refresh() {
  let st;
  try { st = await api("/api/state"); } catch { return; }
  S.state = st;
  renderMeters();
  renderFleet();
}

function renderMeters() {
  const sum = S.state.summary;
  const parts = [`today <b>${money(sum.today)}</b>`];
  if (sum.block) parts.push(`block <b>${money(sum.block.cost)}</b> ↻${until(sum.block.end)}`);
  for (const l of sum.limits) {
    if (l.limit_usd) continue;
    parts.push(`${esc(l.agent)} ${esc(l.name)} <span class="${level(l.used_pct / 100)}">${Math.round(l.used_pct)}%</span>`);
  }
  $("#meters").innerHTML = parts.join("<span>·</span>");
}

function renderFleet() {
  const st = S.state;
  const wins = st.windows;
  $("#fleet-count").textContent = wins.length || "";
  const busy = wins.filter((w) => w.state === "busy").length, wait = wins.filter((w) => w.state === "input").length;
  $("#running-sum").textContent = wins.length ? `${busy} busy · ${wait} need input` : "";
  if (!wins.find((w) => w.id === S.selected)) S.selected = wins[0]?.id || "";

  $("#windows").innerHTML = wins.map((w) => `
    <button class="win ${w.id === S.selected ? "on" : ""}" data-id="${esc(w.id)}">
      <div class="l1"><strong>${esc(w.name)}</strong>${stateBadge(w.state)}</div>
      <div class="l2">${agentChip(w.agent)}${w.external ? ` <span class="chip ext" title="Runs in your own tmux; optimus shows and drives it where it is">↗ your tmux</span>` : ""} ${esc(short(w.worktree ? w.worktree.repo : w.cwd))} · ${ago(w.activity)}${w.cost ? ` · <span class="${st.session_budget && w.cost >= st.session_budget ? "over" : "cost"}">${money(w.cost)}</span>` : ""}</div>
      ${w.worktree ? `<div class="wt">⎇ ${esc(w.worktree.label)}</div>` : ""}
      ${w.state === "input" && w.message ? `<div class="ask">◆ ${esc(w.message)}</div>` : w.title ? `<div class="l3">${esc(w.title)}</div>` : ""}
    </button>`).join("");
  $$("#windows .win").forEach((b) => b.addEventListener("click", () => select(b.dataset.id)));

  const unlinked = st.outside.filter((o) => !o.linked);
  $("#outside-wrap").hidden = !unlinked.length;
  $("#outside").innerHTML = unlinked.map((o) => `
    <div class="o">
      <div>${agentChip(o.agent)} ${esc(o.status || "")} · ${esc(short(o.cwd))}</div>
      <div class="dim">${esc(o.title || "")}${o.title ? " · " : ""}in ${esc(o.where)}</div>
      ${o.can_take_over ? `<button class="take" data-pid="${o.pid}" data-busy="${o.status === "busy" ? 1 : ""}" title="Stop it there and continue the same session inside optimus">Take over</button>` : `<span class="dim">${esc(o.why)}</span>`}
    </div>`).join("");
  $$("#outside .take").forEach((b) => b.addEventListener("click", () => takeOver(+b.dataset.pid, !!b.dataset.busy)));

  const def = st.default_agent;
  $("#quick").innerHTML = st.projects.filter((p) => p.exists).slice(0, 8).map((p) =>
    `<button data-dir="${esc(p.cwd)}" title="Start ${esc(def)} in ${esc(short(p.cwd))}"><span>＋ ${esc(p.name)}</span><span class="dim">${esc(def)}</span></button>`).join("");
  $$("#quick button").forEach((b) => b.addEventListener("click", () => openNew({ dir: b.dataset.dir })));

  const installed = st.agents.filter((a) => a.installed && a.name !== "shell");
  $("#empty-agents").innerHTML = installed.map((a) =>
    `<button data-agent="${esc(a.name)}" class="ag-${esc(a.name)}">${esc(a.name)}<small>${a.name === def ? "default" : esc(a.command)}</small></button>`).join("");
  $$("#empty-agents button").forEach((b) => b.addEventListener("click", () => openNew({ agent: b.dataset.agent })));

  $("#empty").hidden = wins.length > 0;
  $("#pane").hidden = wins.length === 0;
  const sg = st.suggestions?.[0];
  $("#suggest").hidden = !sg;
  if (sg) {
    $("#suggest").innerHTML = `<b>◆ ${esc(sg.from)} ${esc(sg.quota)} quota at ${Math.round(sg.used_pct)}%</b> <span>continue <b>${esc(sg.name)}</b> in ${esc(sg.to)} with its context?</span><button class="primary" id="suggest-go">Continue in ${esc(sg.to)}</button>`;
    $("#suggest-go").onclick = () => {
      if (confirm(`Start ${sg.to} in ${short(sg.cwd)} with the context of ${sg.name}?`))
        act(() => api(`/api/sessions/${encodeURIComponent(sg.session_id)}/handoff`, { method: "POST", body: { target: "new:" + sg.to, dir: sg.cwd } }), `Started ${sg.to} with the context of ${sg.name}`)
          .then((r) => { if (r?.window) select(r.window); });
    };
  }
  const waitingWins = wins.filter((x) => x.state === "input");
  $("#next-btn").hidden = waitingWins.length === 0;
  $("#next-count").textContent = waitingWins.length;
  document.title = waitingWins.length ? `(${waitingWins.length}) Optimus` : "Optimus";
  notifyChanges(wins);
  const w = wins.find((x) => x.id === S.selected);
  if (w) {
    $("#pane-ask").hidden = w.state !== "input";
    $("#pane-ask").innerHTML = w.state === "input" ? `<b>◆ needs you</b> ${esc(w.message || "waiting for an answer")} — answer in the terminal or with the keys below` : "";
    $("#pane-name").textContent = w.name;
    $("#pane-agent").className = "chip ag-" + w.agent;
    $("#pane-agent").textContent = w.agent;
    $("#pane-cwd").textContent = short(w.cwd);
    $("#pane-state").outerHTML = stateBadge(w.state).replace('class="state', 'id="pane-state" class="state');
    for (const b of ["#act-diff", "#act-merge", "#act-discard"]) $(b).hidden = !w.worktree;
    $("#act-kill").hidden = $("#act-rename").hidden = !!w.external;
    $("#act-takeover").hidden = !w.external;
    $("#act-transcript").disabled = !w.session_id;
    $("#act-handoff").disabled = !w.session_id;
    if (term.attached !== w.id) connect(w.id);
  } else {
    disconnect();
  }
}

// ---------------------------------------------------------------- attention

const lastState = {};
function notifyChanges(wins) {
  for (const w of wins) {
    const prev = lastState[w.id];
    lastState[w.id] = w.state;
    if (prev === undefined || prev === w.state) continue;
    if (w.state === "input") alertUser(`${w.name} needs input`, w.message || "waiting for your answer", w.id);
    else if (w.state === "idle" && prev === "busy") alertUser(`${w.name} is done`, w.message && w.message !== "finished" ? w.message : "finished its turn", w.id);
  }
}

function alertUser(title, body, id) {
  if ("Notification" in window && Notification.permission === "granted" && document.hidden) {
    const n = new Notification(title, { body, tag: id, icon: "/static/icon.svg" });
    n.onclick = () => { window.focus(); select(id); n.close(); };
  } else if (!document.hidden) {
    toast(`${title}: ${body}`);
  }
  if (navigator.vibrate && title.includes("needs")) navigator.vibrate([120, 60, 120]);
}

function nextWaiting() {
  const wins = (S.state?.windows || []).filter((w) => w.state === "input");
  if (!wins.length) return toast("No agent needs input");
  const i = wins.findIndex((w) => w.id === S.selected);
  select(wins[(i + 1) % wins.length].id);
  setView("fleet");
}
$("#next-btn").addEventListener("click", nextWaiting);

if ("Notification" in window && window.isSecureContext && Notification.permission === "default") {
  $("#notif-btn").hidden = false;
  $("#notif-btn").addEventListener("click", async () => {
    const p = await Notification.requestPermission();
    $("#notif-btn").hidden = true;
    toast(p === "granted" ? "Notifications on: you'll be told when an agent needs you" : "Notifications blocked");
  });
}

function select(id) {
  if (id === S.selected && term.link === "lost") term.attached = ""; // clicking a dead view reconnects it
  S.selected = id;
  localStorage.setItem("optimus.win", id);
  renderFleet();
  // keystrokes should go to the agent, not the page (no keyboard pop-up on phones)
  if (!isTouch) setTimeout(() => term.xterm?.focus(), 0);
}

// ---------------------------------------------------------------- terminal

const term = { xterm: null, fit: null, ws: null, attached: "" };

function ensureTerm() {
  if (term.xterm) return;
  const css = getComputedStyle(document.documentElement);
  term.xterm = new Terminal({
    fontFamily: css.getPropertyValue("--mono"),
    fontSize: window.innerWidth < 760 ? 11 : 13,
    cursorBlink: true,
    allowProposedApi: true,
    scrollback: 5000,
    theme: { background: "#11111b", foreground: "#cdd6f4", cursor: "#f5e0dc", selectionBackground: "#45475a",
      black: "#45475a", red: "#f38ba8", green: "#a6e3a1", yellow: "#f9e2af", blue: "#89b4fa", magenta: "#f5c2e7", cyan: "#94e2d5", white: "#bac2de",
      brightBlack: "#585b70", brightRed: "#f38ba8", brightGreen: "#a6e3a1", brightYellow: "#f9e2af", brightBlue: "#89b4fa", brightMagenta: "#f5c2e7", brightCyan: "#94e2d5", brightWhite: "#a6adc8" },
  });
  term.fit = new FitAddon.FitAddon();
  term.xterm.loadAddon(term.fit);
  term.xterm.open($("#term"));
  // Ctrl/Cmd+K opens the palette instead of reaching the agent
  term.xterm.attachCustomKeyEventHandler((e) => !((e.ctrlKey || e.metaKey) && e.key.toLowerCase() === "k"));
  term.xterm.onData((d) => term.ws?.readyState === 1 && term.ws.send(new TextEncoder().encode(d)));
  new ResizeObserver(() => fit()).observe($("#term"));
}

function fit() {
  if (!term.xterm || $("#pane").hidden || S.view !== "fleet") return;
  try { term.fit.fit(); } catch { return; }
  if (term.ws?.readyState === 1) term.ws.send(JSON.stringify({ type: "resize", cols: term.xterm.cols, rows: term.xterm.rows }));
}

function disconnect() {
  if (term.ws) { term.ws.onclose = null; term.ws.close(); term.ws = null; }
  term.attached = "";
}

const isTouch = matchMedia("(pointer: coarse)").matches;

function setLink(state) {
  term.link = state;
  const el = $("#pane-link");
  if (!el) return;
  el.className = "link " + state;
  el.title = { live: "Live terminal", connecting: "Connecting…", lost: "Connection lost — click to reconnect" }[state] || "";
}

function connect(id) {
  ensureTerm();
  disconnect();
  term.attached = id;
  term.xterm.reset();
  fit();
  setLink("connecting");
  const proto = location.protocol === "https:" ? "wss" : "ws";
  const ws = new WebSocket(`${proto}://${location.host}/api/term/${encodeURIComponent(id)}?cols=${term.xterm.cols}&rows=${term.xterm.rows}`);
  ws.binaryType = "arraybuffer";
  term.lastMsg = Date.now();
  // every handler checks it still belongs to the current connection: after a
  // switch, late output from the previous agent must never reach the screen
  ws.onmessage = (e) => {
    if (term.ws !== ws) return;
    term.lastMsg = Date.now();
    if (typeof e.data === "string") {
      if (e.data.startsWith("{")) return; // control message (heartbeat)
      term.xterm.write(e.data);
    } else term.xterm.write(new Uint8Array(e.data));
  };
  ws.onopen = () => {
    if (term.ws !== ws) return;
    setLink("live");
    fit();
    if (!isTouch && !document.querySelector("dialog[open]") && !/INPUT|TEXTAREA/.test(document.activeElement?.tagName)) term.xterm.focus();
  };
  ws.onclose = () => {
    if (term.ws !== ws) return;
    setLink("lost");
    term.xterm.write("\r\n\x1b[2m[connection lost — reconnecting…]\x1b[0m\r\n");
    term.attached = "";
    term.ws = null;
    setTimeout(refresh, 1500);
  };
  term.ws = ws;
}

// Reconnect when the connection died silently (laptop sleep, phone in the
// background, a network or Tailscale path dropping): the server sends a
// heartbeat every 15s, so 40s of silence means the link is gone.
function checkLink(force) {
  if (!S.selected || S.view !== "fleet") return;
  const stale = !term.ws || term.ws.readyState > 1 || Date.now() - (term.lastMsg || 0) > 40000;
  if (force || stale) {
    term.attached = "";
    connect(S.selected);
  }
}
setInterval(() => { if (!document.hidden) checkLink(false); }, 5000);
document.addEventListener("visibilitychange", () => { if (!document.hidden) { refresh(); checkLink(false); } });
window.addEventListener("online", () => checkLink(true));

// phone-friendly keys for answering prompts without a keyboard
const KEYS = [["Esc", "Escape"], ["↵", "Enter"], ["↑", "Up"], ["↓", "Down"], ["Tab", "Tab"], ["⇧Tab", "BTab"], ["1", "1"], ["2", "2"], ["3", "3"], ["y", "y"], ["n", "n"], ["^C", "C-c"]];
$("#keys").innerHTML = KEYS.map(([l, k]) => `<button data-key="${k}" title="${k}">${l}</button>`).join("");
$$("#keys button").forEach((b) => b.addEventListener("click", () => {
  if (S.selected) act(() => api(`/api/windows/${encodeURIComponent(S.selected)}/keys`, { method: "POST", body: { keys: [b.dataset.key] } }));
  term.xterm?.focus();
}));

if (window.innerWidth < 760) $("#prompt").placeholder = "Message this agent…";
$("#prompt").addEventListener("input", (e) => { e.target.style.height = "auto"; e.target.style.height = e.target.scrollHeight + "px"; });
$("#prompt").addEventListener("keydown", (e) => {
  if (e.key === "Enter" && !e.shiftKey && !e.isComposing) { e.preventDefault(); $("#prompt-form").requestSubmit(); }
});
$("#prompt-form").addEventListener("submit", async (e) => {
  e.preventDefault();
  const text = $("#prompt").value.trim();
  if (!text) return;
  const all = $("#broadcast").checked;
  const targets = all ? S.state.windows.filter((w) => w.state !== "exited") : S.state.windows.filter((w) => w.id === S.selected);
  for (const w of targets) await act(() => api(`/api/windows/${encodeURIComponent(w.id)}/send`, { method: "POST", body: { text } }));
  toast(all ? `Sent to ${targets.length} agents` : `Sent to ${targets[0]?.name}`);
  $("#prompt").value = "";
  $("#prompt").style.height = "auto";
});

$("#pane-link").addEventListener("click", () => checkLink(true));

$("#act-kill").addEventListener("click", () => {
  const w = S.state.windows.find((x) => x.id === S.selected);
  if (w && confirm(`Stop ${w.name}?`)) act(() => api(`/api/windows/${encodeURIComponent(w.id)}`, { method: "DELETE" }), `Stopped ${w.name}`);
});
$("#act-rename").addEventListener("click", () => {
  const w = S.state.windows.find((x) => x.id === S.selected);
  const name = w && prompt("Rename window", w.name);
  if (name) act(() => api(`/api/windows/${encodeURIComponent(w.id)}/rename`, { method: "POST", body: { name } }));
});
async function takeOver(pid, busy) {
  const o = S.state.outside.find((x) => x.pid === pid);
  const what = o ? `${o.agent} in ${short(o.cwd)}${o.title ? ` (“${o.title}”)` : ""}` : `pid ${pid}`;
  if (!confirm(`Take over ${what}?\n\nIt will be stopped where it runs now and the same session continues inside optimus.`)) return;
  let force = false;
  if (busy && !(force = confirm("It's in the middle of a turn. Stopping it now cuts that turn short. Stop it anyway?"))) return;
  const r = await act(() => api(`/api/outside/${pid}/takeover`, { method: "POST", body: { force } }), "Now running in optimus");
  if (r?.window) { S.selected = r.window; setView("fleet"); }
}
$("#act-takeover").addEventListener("click", () => {
  const w = selectedWin();
  const o = w && S.state.outside.find((x) => x.linked === w.id);
  if (o) takeOver(o.pid, o.status === "busy");
});

const selectedWin = () => S.state.windows.find((x) => x.id === S.selected);
$("#act-diff").addEventListener("click", () => openDiff(selectedWin()));
$("#act-merge").addEventListener("click", () => mergeWin(selectedWin()));
$("#act-discard").addEventListener("click", () => {
  const w = selectedWin();
  if (w && confirm(`Stop ${w.name} and DISCARD its worktree and branch (${w.worktree.label}), including unmerged work?`))
    act(() => api(`/api/windows/${encodeURIComponent(w.id)}/discard`, { method: "POST", body: {} }), `Discarded ${w.worktree.branch}`);
});
$("#diff-close").addEventListener("click", () => $("#dlg-diff").close());
$("#diff-merge").addEventListener("click", () => { $("#dlg-diff").close(); mergeWin(S.diffWin); });

function mergeWin(w) {
  if (w && confirm(`Commit and merge ${w.name}'s work (${w.worktree.label}) into ${short(w.worktree.repo)}?`))
    act(() => api(`/api/windows/${encodeURIComponent(w.id)}/merge`, { method: "POST", body: {} }), (r) => `Merged ${r.merged} into ${short(r.into)}`);
}

async function openDiff(w) {
  if (!w) return;
  let r;
  try { r = await api(`/api/windows/${encodeURIComponent(w.id)}/diff`); } catch (e) { return toast(e.message, true); }
  S.diffWin = w;
  $("#diff-title").textContent = `${w.name} — ${w.worktree.label}`;
  $("#diff-meta").textContent = `${r.branch} · ${short(w.worktree.path)}`;
  $("#diff-body").innerHTML = (r.diff || "No changes yet.").split("\n").map((l) => {
    const c = /^(\+\+\+|---|diff --git)/.test(l) ? "f" : l.startsWith("@@") ? "h" : l.startsWith("+") ? "a" : l.startsWith("-") ? "d" : "";
    return c ? `<span class="${c}">${esc(l)}</span>` : esc(l);
  }).join("\n");
  $("#dlg-diff").showModal();
}

$("#act-transcript").addEventListener("click", () => {
  const w = S.state.windows.find((x) => x.id === S.selected);
  if (w?.session_id) openTranscript(w.session_id);
});
$("#act-handoff").addEventListener("click", () => {
  const w = S.state.windows.find((x) => x.id === S.selected);
  if (w?.session_id) openHandoff({ id: w.session_id, title: w.title || w.name, cwd: w.cwd });
});

// ---------------------------------------------------------------- new session

function openNew({ agent, dir } = {}) {
  const st = S.state;
  if (!st) return;
  if (!st.tmux) return toast("tmux is not installed on the optimus host", true);
  S.newAgent = agent || S.newAgent || st.default_agent;
  $("#new-agents").innerHTML = st.agents.map((a) =>
    `<button type="button" data-agent="${esc(a.name)}" class="ag-${esc(a.name)} ${a.name === S.newAgent ? "on" : ""}" ${a.installed ? "" : "disabled title='not installed'"}>${esc(a.name)}</button>`).join("");
  S.fanAgents = new Set([S.newAgent]);
  $("#new-fan").checked = false;
  $("#new-wt").checked = false;
  $$("#new-agents button").forEach((b) => b.addEventListener("click", () => {
    if ($("#new-fan").checked) {
      S.fanAgents.has(b.dataset.agent) ? S.fanAgents.delete(b.dataset.agent) : S.fanAgents.add(b.dataset.agent);
      b.classList.toggle("on", S.fanAgents.has(b.dataset.agent));
    } else {
      S.newAgent = b.dataset.agent;
      $$("#new-agents button").forEach((x) => x.classList.toggle("on", x === b));
    }
    $("#new-rc-wrap").hidden = $("#new-fan").checked || S.newAgent !== "claude";
  }));
  const dirs = st.projects.filter((p) => p.exists);
  $("#new-dirs").innerHTML = dirs.map((p) => `<option value="${esc(short(p.cwd))}">`).join("");
  $("#new-recent").innerHTML = dirs.slice(0, 6).map((p) => `<button type="button" data-dir="${esc(short(p.cwd))}">${esc(p.name)}</button>`).join("");
  $$("#new-recent button").forEach((b) => b.addEventListener("click", () => { $("#new-dir").value = b.dataset.dir; $("#new-prompt").focus(); }));
  const cur = st.windows.find((w) => w.id === S.selected);
  $("#new-dir").value = dir ? short(dir) : short(cur?.cwd || dirs[0]?.cwd || st.cwd);
  $("#new-prompt").value = "";
  $("#new-name").value = "";
  $("#new-rc").checked = st.claude_remote_control;
  $("#new-rc-wrap").hidden = S.newAgent !== "claude";
  $("#dlg-new").showModal();
  $("#new-prompt").focus();
}

$("#new-btn").addEventListener("click", () => openNew());
$("#new-fan").addEventListener("change", () => {
  const fan = $("#new-fan").checked;
  $("#new-wt").checked = fan || $("#new-wt").checked;
  $("#new-wt").disabled = fan;
  S.fanAgents = new Set([S.newAgent]);
  $$("#new-agents button").forEach((x) => x.classList.toggle("on", x.dataset.agent === S.newAgent));
  $("#new-go").firstChild.textContent = fan ? "Fan out " : "Start ";
});
$("#new-form").addEventListener("keydown", (e) => {
  if (e.key === "Enter" && (e.ctrlKey || e.metaKey)) { e.preventDefault(); $("#new-go").click(); }
});
$("#new-form").addEventListener("submit", async (e) => {
  if (e.submitter?.value !== "go") return;
  e.preventDefault();
  const dir = $("#new-dir").value.trim(), prompt = $("#new-prompt").value.trim();
  $("#new-go").disabled = true;
  let r;
  if ($("#new-fan").checked) {
    if (!prompt) { $("#new-go").disabled = false; return toast("Fan-out needs a task for the agents", true); }
    const agents = [...S.fanAgents];
    r = await act(() => api("/api/fanout", { method: "POST", body: { agents, dir, prompt } }), `Fanned out to ${agents.join(", ")}`);
    if (r) r.window = r.windows[0];
  } else {
    const body = { agent: S.newAgent, dir, prompt, name: $("#new-name").value.trim(), worktree: $("#new-wt").checked };
    if (S.newAgent === "claude") body.remote_control = $("#new-rc").checked;
    r = await act(() => api("/api/windows", { method: "POST", body }), `Started ${S.newAgent}`);
  }
  $("#new-go").disabled = false;
  if (r) {
    $("#dlg-new").close();
    S.selected = r.window;
    setView("fleet");
  }
});

// ---------------------------------------------------------------- sessions

let sTimer;
["#s-q", "#s-agent", "#s-project", "#s-content", "#s-auto"].forEach((s) => $(s).addEventListener("input", () => { clearTimeout(sTimer); sTimer = setTimeout(loadSessions, 180); }));

async function loadSessions() {
  const st = S.state;
  if (st) {
    const agents = [...new Set(st.agents.filter((a) => a.history).map((a) => a.name))];
    const curA = $("#s-agent").value, curP = $("#s-project").value;
    $("#s-agent").innerHTML = `<option value="">All agents</option>` + agents.map((a) => `<option ${a === curA ? "selected" : ""}>${esc(a)}</option>`).join("");
    $("#s-project").innerHTML = `<option value="">All projects</option>` + st.projects.map((p) => `<option value="${esc(p.cwd)}" ${p.cwd === curP ? "selected" : ""}>${esc(p.name)} — ${esc(short(p.cwd))}</option>`).join("");
  }
  const inside = $("#s-content").checked;
  const q = new URLSearchParams({ q: $("#s-q").value, agent: $("#s-agent").value, cwd: $("#s-project").value, content: inside ? "1" : "", automated: $("#s-auto").checked ? "1" : "" });
  if (inside && $("#s-q").value.trim().length < 3) { $("#s-sum").textContent = "type at least 3 characters to search inside transcripts"; $("#s-body").innerHTML = ""; return; }
  let r;
  try { r = await api("/api/sessions?" + q); } catch (e) { return; }
  S.sessions = r.sessions;
  $("#s-sum").textContent = `${r.sessions.length} sessions · ${money(r.total_cost)}` + (r.automated_hidden ? ` · ${r.automated_hidden} automated hidden` : "");
  const term = $("#s-q").value.trim();
  const mark = (t) => esc(t).replace(new RegExp(term.replace(/[.*+?^${}()|[\]\\]/g, "\\$&"), "gi"), (m) => `<mark>${m}</mark>`);
  $("#s-body").innerHTML = r.sessions.map((s, i) => `
    <tr data-i="${i}">
      <td>${s.live ? '<span class="live-dot" title="running">●</span>' : ""}</td>
      <td>${agentChip(s.agent)}</td>
      <td class="dim">${ago(s.end)}</td>
      <td class="hide-sm">${esc(s.project)}</td>
      <td class="title">${s.snippet ? `<span class="snip">“${mark(s.snippet)}”</span><br><span class="dim">${esc(s.title)}</span>` : (s.automated ? `<span class="auto">⚙ </span>` : "") + esc(s.title)}</td>
      <td class="num hide-sm">${s.turns}</td>
      <td class="num hide-sm">${tokens(s.tokens)}</td>
      <td class="money r">${money(s.cost)}</td>
      <td><div class="row-actions">${s.resumable ? `<button data-act="resume">Resume</button>` : ""}<button data-act="handoff">Handoff</button></div></td>
    </tr>`).join("") || `<tr><td colspan="9" class="dim">No sessions match.</td></tr>`;
  $$("#s-body tr[data-i]").forEach((tr) => tr.addEventListener("click", (e) => {
    const s = S.sessions[+tr.dataset.i];
    const a = e.target.closest("button")?.dataset.act;
    if (a === "resume") resume(s.id);
    else if (a === "handoff") openHandoff(s);
    else openTranscript(s.id);
  }));
}

async function resume(id) {
  const r = await act(() => api(`/api/sessions/${encodeURIComponent(id)}/resume`, { method: "POST" }), "Resumed in the fleet");
  if (r) { $("#dlg-transcript").close(); S.selected = r.window; setView("fleet"); }
}

async function openTranscript(id) {
  let r;
  try { r = await api(`/api/sessions/${encodeURIComponent(id)}/transcript`); } catch (e) { return toast(e.message, true); }
  const s = r.session;
  S.transcriptOf = s;
  $("#tr-title").textContent = s.title;
  $("#tr-meta").innerHTML = `${agentChip(s.agent)} ${esc(short(s.cwd))} · ${esc(s.model)} · ${s.turns} turns · ${money(s.cost)} · <span class="mono">${esc(s.short_id)}</span>`;
  $("#tr-resume").hidden = !s.resumable;
  const rel = (t) => (t && s.cwd && t.startsWith(s.cwd + "/") ? t.slice(s.cwd.length + 1) : short(t));
  $("#tr-body").innerHTML = r.messages.map((m) => `
    <div class="msg ${m.role === "user" ? "user" : "assistant"}">
      <div class="who">${m.role === "user" ? "you" : esc(s.agent)} <span class="dim">${m.time && !m.time.startsWith("0001") ? new Date(m.time).toLocaleString() : ""}</span></div>
      ${m.text ? `<div class="body">${esc(m.text)}</div>` : ""}
      ${(m.tools || []).map((t) => `<div class="tool ${t.Edit ? "edit" : ""}">${t.Edit ? "✎" : "⚙"} ${esc(t.Name)} ${esc(rel(t.Target))}</div>`).join("")}
    </div>`).join("") || `<p class="dim">Empty transcript.</p>`;
  $("#dlg-transcript").showModal();
  $("#tr-body").scrollTop = $("#tr-body").scrollHeight;
}
$("#tr-close").addEventListener("click", () => $("#dlg-transcript").close());
$("#tr-resume").addEventListener("click", () => resume(S.transcriptOf.id));
$("#tr-handoff").addEventListener("click", () => { $("#dlg-transcript").close(); openHandoff(S.transcriptOf); });

// ---------------------------------------------------------------- handoff

function openHandoff(s) {
  const st = S.state;
  S.handoffOf = s;
  $("#handoff-what").textContent = `From “${s.title}” (${short(s.cwd)}). The receiving agent gets a document with the goal, files touched, recent conversation and where it left off.`;
  const targets = [];
  for (const a of st.agents.filter((x) => x.installed && x.name !== "shell")) targets.push([`new:${a.name}`, `New <b class="ag-${esc(a.name)}">${esc(a.name)}</b> session`, `in ${esc(short(s.cwd))}`]);
  for (const w of st.windows.filter((x) => x.state !== "exited")) targets.push([`win:${w.id}`, `Into ${esc(w.name)}`, `${esc(w.agent)} · running`]);
  targets.push(["file", "Save document", "and copy it"]);
  $("#handoff-targets").innerHTML = targets.map(([v, l, d]) => `<button type="button" data-t="${esc(v)}"><span>${l}</span><span class="dim">${d}</span></button>`).join("");
  $$("#handoff-targets button").forEach((b) => b.addEventListener("click", () => doHandoff(b.dataset.t)));
  $("#handoff-note").value = "";
  $("#handoff-sum").checked = false;
  $("#handoff-doc").value = "";
  $("#handoff-doc").dataset.edited = "";
  $("#handoff-preview-wrap").open = false;
  $("#dlg-handoff").showModal();
}

$("#handoff-preview-wrap").addEventListener("toggle", async () => {
  if (!$("#handoff-preview-wrap").open || $("#handoff-doc").value) return;
  try {
    const r = await api(`/api/sessions/${encodeURIComponent(S.handoffOf.id)}/handoff`, { method: "POST", body: { target: "preview", note: $("#handoff-note").value.trim(), summarize: $("#handoff-sum").checked } });
    $("#handoff-doc").value = r.document;
  } catch (e) { toast(e.message, true); }
});
$("#handoff-doc").addEventListener("input", () => ($("#handoff-doc").dataset.edited = "1"));

async function doHandoff(target) {
  const s = S.handoffOf;
  const body = { target, note: $("#handoff-note").value.trim(), summarize: $("#handoff-sum").checked };
  if ($("#handoff-doc").dataset.edited) body.document = $("#handoff-doc").value;
  $$("#handoff-targets button").forEach((b) => (b.disabled = true));
  if (body.summarize) toast("Condensing with an agent… this can take a minute");
  const r = await act(() => api(`/api/sessions/${encodeURIComponent(s.id)}/handoff`, { method: "POST", body }));
  $$("#handoff-targets button").forEach((b) => (b.disabled = false));
  if (!r) return;
  $("#dlg-handoff").close();
  if (r.document) {
    try { await navigator.clipboard.writeText(r.document); toast("Handoff copied to clipboard · saved " + short(r.path)); }
    catch { toast("Saved " + short(r.path)); }
  } else {
    toast("Context handed off · " + short(r.path));
  }
  if (r.window) { S.selected = r.window; setView("fleet"); }
  else if (target.startsWith("win:")) { S.selected = target.slice(4); setView("fleet"); }
}

// ---------------------------------------------------------------- usage

async function loadUsage() {
  let u;
  try { u = await api("/api/usage?days=30"); } catch { return; }
  const sum = S.state?.summary;
  if (sum) {
    $("#u-cards").innerHTML = [["Today", sum.today], ["This week", sum.week], ["This month", sum.month], ["Last 30 days", u.total]]
      .map(([k, v]) => `<div class="card"><div class="k">${k}</div><div class="v">${money(v)}</div></div>`).join("");
    $("#u-limits").innerHTML = sum.limits.length ? sum.limits.map((l) => meter(`${l.agent} ${l.name}`, l.used_pct / 100,
      `${Math.round(l.used_pct)}% used${l.resets_at && !l.resets_at.startsWith("0001") ? " · ↻ " + until(l.resets_at) : ""}`)).join("")
      : `<p class="dim">No quota data yet. Run <code>optimus config statusline --install</code> so Claude Code reports its 5h / 7d windows.</p>`;
    if (sum.block) {
      const b = sum.block, el = Date.now() - new Date(b.start), total = new Date(b.end) - new Date(b.start);
      const rate = b.cost / Math.max(el / 3.6e6, 0.25);
      $("#u-block").innerHTML = meter("elapsed", el / total, `${dur(el)} of ${dur(total)}`) +
        `<div class="kv"><span class="dim">spent</span><span>${money(b.cost)} · ${tokens(b.tokens)} tokens</span>
         <span class="dim">burn rate</span><span>${money(rate)}/h → ${money(b.cost + (rate * (new Date(b.end) - Date.now())) / 3.6e6)} projected</span>
         <span class="dim">resets</span><span>${new Date(b.end).toLocaleTimeString([], { hour: "2-digit", minute: "2-digit" })} (in ${until(b.end)})</span></div>`;
    } else $("#u-block").innerHTML = `<p class="dim">No active block.</p>`;
    $("#u-budgets").innerHTML = sum.budgets.length ? sum.budgets.map((b) => meter(b.Name, b.Limit ? b.Spent / b.Limit : 0, `${money(b.Spent)} / ${money(b.Limit)}`)).join("")
      : `<p class="dim">None set — <code>optimus config set budgets.daily_usd 50</code></p>`;
  }
  const max = Math.max(...u.daily.map((d) => d.cost), 0.0001);
  $("#u-chart").innerHTML = u.daily.map((d, i) => `<div class="c" title="${esc(d.key)}: ${money(d.cost)}"><i style="height:${(d.cost / max) * 100}%"></i><span>${i % 5 === 4 || i === u.daily.length - 1 ? esc(d.key.slice(8)) : "&nbsp;"}</span></div>`).join("");
  const rank = (rows, fmt) => `<div class="rank">${rows.slice(0, 10).map((r) => `<span>${fmt(r)}</span><span class="num">${tokens(r.tokens)}</span><span class="money">${money(r.cost)}</span>`).join("")}</div>`;
  $("#u-model").innerHTML = rank(u.by_model, (r) => esc(r.key) + (r.priced ? "" : ' <span class="dim">(unpriced)</span>'));
  $("#u-agent").innerHTML = rank(u.by_agent, (r) => agentChip(r.key));
  $("#u-project").innerHTML = rank(u.by_project, (r) => esc(short(r.key)));
}

function meter(label, frac, val) {
  return `<div class="meter"><span>${esc(label)}</span><div class="bar"><i class="${level(frac)}" style="width:${Math.min(frac, 1) * 100}%"></i></div><span class="val">${esc(val)}</span></div>`;
}

// ---------------------------------------------------------------- login & boot

function showLogin() {
  if (!$("#dlg-login").open) $("#dlg-login").showModal();
}
$("#login-form").addEventListener("submit", (e) => {
  e.preventDefault();
  location.href = "/?token=" + encodeURIComponent($("#login-token").value.trim());
});

document.addEventListener("keydown", (e) => {
  const typing = /INPUT|TEXTAREA|SELECT/.test(document.activeElement?.tagName) || document.activeElement?.closest(".xterm") || $("dialog[open]");
  if (typing || e.ctrlKey || e.metaKey || e.altKey) return;
  if (e.key === "n") { e.preventDefault(); openNew(); }
  else if (e.key === "i") { e.preventDefault(); nextWaiting(); }
  else if (e.key === "1") setView("fleet");
  else if (e.key === "2") setView("sessions");
  else if (e.key === "3") setView("usage");
  else if (e.key === "/" && S.view === "sessions") { e.preventDefault(); $("#s-q").focus(); }
});

// ---------------------------------------------------------------- command palette

const PAL = { items: [], shown: [], cur: 0, sessions: [] };

async function openPalette() {
  const st = S.state;
  if (!st) return;
  const items = [];
  for (const w of st.windows) items.push({ k: "agent", l: `go to ${w.name}`, d: `${w.agent} · ${w.state}${w.message ? ": " + w.message : ""}`, run: () => { select(w.id); setView("fleet"); } });
  const acts = [
    ["new session…", () => openNew()], ["next agent waiting for me", nextWaiting], ["open on my phone", () => $("#phone-btn").click()],
    ["view: fleet", () => setView("fleet")], ["view: sessions", () => setView("sessions")], ["view: usage", () => setView("usage")],
    ["search inside transcripts", () => { setView("sessions"); $("#s-content").checked = true; $("#s-q").focus(); }],
  ];
  for (const [l, run] of acts) items.push({ k: "action", l, d: "", run });
  for (const p of st.projects.filter((p) => p.exists).slice(0, 8))
    for (const a of st.agents.filter((a) => a.installed && a.name !== "shell"))
      items.push({ k: "start", l: `new ${a.name} in ${p.name}`, d: short(p.cwd), run: () => openNew({ agent: a.name, dir: p.cwd }) });
  try {
    if (!PAL.sessions.length) PAL.sessions = (await api("/api/sessions?limit=60")).sessions;
  } catch {}
  for (const s of PAL.sessions) items.push({ k: "session", l: s.title, d: `${s.agent} · ${s.project} · ${ago(s.end)}`, run: () => openTranscript(s.id) });
  PAL.items = items;
  $("#pal-q").value = "";
  renderPalette();
  $("#dlg-palette").showModal();
  $("#pal-q").focus();
}

function renderPalette() {
  const words = $("#pal-q").value.toLowerCase().split(/\s+/).filter(Boolean);
  PAL.shown = PAL.items.filter((it) => words.every((w) => `${it.l} ${it.d} ${it.k}`.toLowerCase().includes(w))).slice(0, 60);
  PAL.cur = Math.min(PAL.cur, Math.max(PAL.shown.length - 1, 0));
  $("#pal-list").innerHTML = PAL.shown.map((it, i) => `<div class="pal-item ${i === PAL.cur ? "on" : ""}" data-i="${i}"><span class="l">${esc(it.l)} <span class="dim">${esc(it.d)}</span></span><span class="k">${esc(it.k)}</span></div>`).join("") || `<p class="dim">No match.</p>`;
  $$("#pal-list .pal-item").forEach((el) => el.addEventListener("click", () => runPalette(+el.dataset.i)));
  $("#pal-list .on")?.scrollIntoView({ block: "nearest" });
}

function runPalette(i) {
  const it = PAL.shown[i];
  $("#dlg-palette").close();
  it?.run();
}

$("#pal-q").addEventListener("input", () => { PAL.cur = 0; renderPalette(); });
$("#pal-q").addEventListener("keydown", (e) => {
  if (e.key === "ArrowDown") { e.preventDefault(); PAL.cur = Math.min(PAL.cur + 1, PAL.shown.length - 1); renderPalette(); }
  else if (e.key === "ArrowUp") { e.preventDefault(); PAL.cur = Math.max(PAL.cur - 1, 0); renderPalette(); }
  else if (e.key === "Enter") { e.preventDefault(); runPalette(PAL.cur); }
});
$("#pal-btn").addEventListener("click", openPalette);
document.addEventListener("keydown", (e) => {
  if ((e.ctrlKey || e.metaKey) && e.key.toLowerCase() === "k") { e.preventDefault(); openPalette(); }
});

// ---------------------------------------------------------------- phone & app

$("#phone-btn").addEventListener("click", async () => {
  let r;
  try { r = await api("/api/phone"); } catch (e) { return toast(e.message, true); }
  $("#phone-body").innerHTML = r.urls?.length
    ? `<p class="dim">Scan with your phone's camera. The link logs it in.</p><div class="qr">${r.svg}</div><div class="phone-url">${esc(r.urls[0].split("/?")[0])}</div>
       <p class="dim">Then use <b>Add to Home Screen</b> to get it as an app. Installing it and system notifications need HTTPS (e.g. <code>tailscale serve</code>).</p>`
    : `<p>This dashboard only listens on <code>${esc(r.addr)}</code>, which your phone can't reach.</p>
       <p class="dim">Restart it on a private network address, for example your Tailscale IP:</p>
       <pre class="mono">optimus web --stop\noptimus web --bg --addr &lt;tailscale-ip&gt;:7777</pre>
       <p class="dim">Then open 📱 again to get a QR code.</p>`;
  $("#dlg-phone").showModal();
});
$("#phone-close").addEventListener("click", () => $("#dlg-phone").close());

// installable app: the service worker needs a secure context (HTTPS or localhost)
if ("serviceWorker" in navigator && window.isSecureContext) navigator.serviceWorker.register("/sw.js").catch(() => {});

refresh();
setInterval(() => { if (!document.hidden) refresh(); }, 2500);
setInterval(() => { if (!document.hidden && S.view === "usage") loadUsage(); }, 30000);
