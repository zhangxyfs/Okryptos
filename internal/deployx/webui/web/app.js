// okdeploy 前端：hash 路由 + SSE 日志。token 经 #token= fragment 进入（同 OkManager 模式）。
// 视觉事实源：docs/prototypes/prototype-okdeploy-final.html（gitignored，本地评审产物）
"use strict";

async function api(path, opts = {}) {
  const o = { method: opts.method || "GET", headers: { "X-Ok-Token": window.OK_TOKEN } };
  if (opts.body !== undefined) {
    o.headers["Content-Type"] = "application/json";
    o.body = JSON.stringify(opts.body);
  }
  const r = await fetch(path, o);
  let data = null;
  if (r.status !== 204) { data = await r.json().catch(() => null); }
  if (!r.ok) {
    const err = new Error((data && data.error) || ("HTTP " + r.status));
    err.payload = data;
    throw err;
  }
  return data;
}

function el(tag, cls, text) {
  const e = document.createElement(tag);
  if (cls) e.className = cls;
  if (text !== undefined) e.textContent = text;
  return e;
}

// ---- 状态 ----
const S = {
  probe: null,      // 最近一次 ProbeResult
  deployDir: "",    // 已部署目录（existing 或部署成功后）
  connStr: "",      // "user@host"（页眉副标题用）
  connHost: "",     // 连接主机（full 模式拼 root_url 用）
  logES: null,
};

// ---- 顶栏连接徽标 ----
function setBadge(on, text) {
  const b = document.getElementById("conn-badge");
  if (!b) return;
  b.className = "badge " + (on ? "on" : "off");
  b.textContent = "";
  const dot = el("span", "dot");
  dot.style.background = on ? "var(--ok)" : "var(--gray)";
  b.append(dot, document.createTextNode(text));
}

// ---- 路由 ----
function route() {
  const raw = location.hash.replace(/^#\/?/, "");
  const page = raw.split("?")[0] || "connect";
  // 渲染前先关掉上一页的 SSE 连接，避免路由切换后旧 EventSource 泄漏
  // （需要日志的页面会在渲染后由 mountLogPane 重新建立连接）。
  if (S.logES) { S.logES.close(); S.logES = null; }
  // 中途刷新会丢内存态会话：非 connect 页在未连接状态下直接跳回连接页，
  // 避免 probe/deploy/manage 拿空状态渲染出畸形数据（如 root_url 拼成 http://:3000/）。
  if (page !== "connect" && !S.connStr) {
    location.hash = "#/connect";
    return;
  }
  const app = document.getElementById("app");
  app.innerHTML = "";
  const content = el("div", "content");
  app.append(content);
  const query = raw.split("?")[1] || "";
  ({ connect: pageConnect, probe: pageProbe, deploy: pageDeploy, manage: pageManage }[page] || pageConnect)(content, query);
}
window.addEventListener("hashchange", route);

// ---- 日志面板（部署/管理共用，SSE 实时滚动）----
// 结构照原型 .logwrap/.dl/.log；日志行 span 级别配色：ok→g、err→r、info→w、step 前缀蓝色。
function mountLogPane(container, onEvent, heightCls) {
  container.innerHTML = "";
  const wrap = el("div", "logwrap");
  const dlw = el("div", "dl");
  const dl = el("button", "btn btn-mini", "下载日志");
  const body = el("div", "log " + (heightCls || "h320"));
  dl.onclick = () => downloadLog(body);
  dlw.append(dl);
  wrap.append(dlw, body);
  container.append(wrap);
  if (S.logES) S.logES.close();
  S.logES = new EventSource("/api/logs/stream?token=" + encodeURIComponent(window.OK_TOKEN));
  S.logES.onmessage = (m) => {
    const ev = JSON.parse(m.data);
    if (ev.step) {
      const s = el("span", "step");
      s.textContent = "[" + ev.step + "] ";
      body.append(s);
    }
    const line = el("span", { ok: "g", err: "r" }[ev.level] || "w");
    line.textContent = ev.text + "\n";
    body.append(line);
    body.scrollTop = body.scrollHeight;
    if (onEvent) onEvent(ev);
  };
  return body;
}

async function downloadLog(body) {
  // 历史全量已在 SSE 首帧补齐，这里直接从面板文本导出
  const blob = new Blob([body ? body.innerText : ""], { type: "text/plain" });
  const a = el("a");
  a.href = URL.createObjectURL(blob);
  a.download = "okdeploy-log-" + new Date().toISOString().slice(0, 19).replace(/[:T]/g, "") + ".txt";
  a.click();
  URL.revokeObjectURL(a.href);
}

// ---- 通用小组件 ----
function alertBar(kind, text) { return el("div", "alert " + kind, text); }

function prow(key, nodes) {
  const row = el("div", "prow");
  row.append(el("span", "k", key));
  for (const n of nodes) row.append(n);
  return row;
}

function pinput(cls, value, width, type) {
  const i = el("input", "pinput" + (cls ? " " + cls : ""));
  if (type) i.type = type;
  i.value = value;
  if (width) i.style.width = width;
  return i;
}

function pcard(title, desc) {
  const c = el("div", "pcard");
  c.append(el("h3", "", title));
  if (desc) c.append(el("div", "pdesc", desc));
  return c;
}

// 一次性密码卡（部署完成 / 重置 root 完成共用组件语义，照原型 pwd-card）
function pwdCard(title, pw, warnText) {
  const card = el("div", "pwd-card");
  const head = el("div");
  head.style.cssText = "font-weight:700;font-size:14.5px";
  head.append(document.createTextNode(title));
  const once = el("span", "muted small", "（只显示这一次）");
  once.style.fontWeight = "400";
  head.append(once);
  const pwe = el("div", "pw", pw);
  const cpw = el("div");
  const copyBtn = el("button", "btn", "复制");
  copyBtn.onclick = async () => {
    try { await navigator.clipboard.writeText(pw); copyBtn.textContent = "已复制"; }
    catch (e) { copyBtn.textContent = "复制失败，请手动选择"; }
  };
  cpw.append(copyBtn);
  const warn = el("div", "fb-err", warnText);
  warn.style.marginTop = "8px";
  card.append(head, pwe, cpw, warn);
  return card;
}

// 目录选择器（部署表单"浏览…"按钮）：/api/ls 列子目录，点选下钻、"选此目录"确认。
function openDirPicker(input) {
  let cur = input.value.trim(); // 空路径列 $HOME（后端约定）
  const mask = el("div", "mask");
  const modal = el("div", "modal");
  mask.append(modal);
  mask.onclick = (e) => { if (e.target === mask) mask.remove(); };
  document.body.append(mask);

  async function load(path) {
    modal.innerHTML = "";
    modal.append(el("h3", "", "选择部署目录"));
    const curRow = el("div", "small muted", "当前：" + (path || "$HOME"));
    curRow.style.marginBottom = "10px";
    modal.append(curRow);
    const list = el("div");
    list.style.cssText = "max-height:220px;overflow-y:auto;margin-bottom:14px";
    modal.append(list);
    const foot = el("div");
    foot.style.cssText = "display:flex;gap:10px;justify-content:flex-end";
    const pick = el("button", "btn btn-primary", "选此目录");
    pick.onclick = () => { input.value = path || "~/openknowledge"; mask.remove(); };
    const cancel = el("button", "btn", "取消");
    cancel.onclick = () => mask.remove();
    foot.append(cancel, pick);
    modal.append(foot);
    let res;
    try { res = await api("/api/ls", { method: "POST", body: { path: path } }); }
    catch (e) { list.append(el("div", "fb-err", e.message)); return; }
    const dirs = res.dirs || [];
    if (!dirs.length) list.append(el("div", "small muted", "（无子目录）"));
    for (const d of dirs) {
      const clean = d.replace(/\/+$/, "");
      const name = clean.split("/").pop() || clean;
      const btn = el("button", "btn btn-block", name + "/");
      btn.style.cssText = "justify-content:flex-start;margin-bottom:6px;font-weight:400";
      btn.onclick = () => load(clean);
      list.append(btn);
    }
  }
  load(cur);
}

/* ================== 连接页 ================== */
function pageConnect(content) {
  content.className = "content narrow";
  const wrap = el("div");
  wrap.style.cssText = "width:640px;margin-top:48px";
  const card = el("div", "pcard");
  card.style.padding = "24px 28px";
  const head = el("h2", "pagehead", "连接到 NAS");
  head.style.cssText = "text-align:center;margin-bottom:2px";
  const sub = el("div", "pagesub", "通过 SSH 部署 OpenKnowledge 服务端");
  sub.style.cssText = "text-align:center;margin-bottom:18px";
  card.append(head, sub);
  const errSlot = el("div");
  card.append(errSlot);

  const hostI = pinput("mono", "", "290px");
  hostI.placeholder = "<user>@<ip> 或 <ip>";
  const portI = pinput("mono", "22", "56px");
  card.append(prow("SSH 地址", [hostI, el("span", "muted small", "端口"), portI]));
  const userI = pinput("", "", "290px");
  userI.placeholder = "地址里写了 user@ 可留空";
  card.append(prow("用户名", [userI]));

  // user@host 一把输：地址框每次输入都实时解析；用户名框的值若等于上次自动填入
  // 的值（或为空）则继续跟随，一旦用户手动改过就不再覆盖。
  let lastAutoUser = "";
  userI.addEventListener("input", () => {
    lastAutoUser = "__manual__"; // 用户碰过用户名框
  });
  hostI.addEventListener("input", () => {
    const v = hostI.value.trim();
    const at = v.indexOf("@");
    if (at > 0) {
      const u = v.slice(0, at);
      if (lastAutoUser !== "__manual__" || !userI.value.trim()) {
        userI.value = u;
        lastAutoUser = u;
      }
    }
  });
  function splitUserHost() {
    const v = hostI.value.trim();
    const at = v.indexOf("@");
    if (at > 0) {
      if (!userI.value.trim()) userI.value = v.slice(0, at);
      return { host: v.slice(at + 1), user: userI.value.trim() };
    }
    return { host: v, user: userI.value.trim() };
  }

  // 认证方式 tabs：密码 / 私钥（二选一）
  const authRow = prow("认证方式", []);
  authRow.style.marginBottom = "2px";
  card.append(authRow);
  const tabs = el("div", "tabs");
  tabs.style.marginLeft = "0";
  const tabPwd = el("button", "on", "密码");
  const tabKey = el("button", "", "私钥");
  tabs.append(tabPwd, tabKey);
  card.append(tabs);
  const authSlot = el("div");
  card.append(authSlot);
  const pwdI = pinput("", "", "290px", "password");
  const keyI = pinput("mono", "", "290px");
  keyI.placeholder = "C:\\Users\\admin\\.ssh\\id_ed25519";
  function showAuth(which) {
    tabPwd.className = which === "pwd" ? "on" : "";
    tabKey.className = which === "key" ? "on" : "";
    authSlot.innerHTML = "";
    if (which === "pwd") {
      authSlot.append(prow("密码", [pwdI]));
    } else {
      authSlot.append(prow("私钥文件", [keyI]));
      const hint = el("div", "small muted", "支持 OpenSSH 格式，passphrase 将在连接时询问");
      hint.style.margin = "-4px 0 4px 120px";
      authSlot.append(hint);
    }
  }
  tabPwd.onclick = () => showAuth("pwd");
  tabKey.onclick = () => showAuth("key");
  showAuth("pwd");

  const btnWrap = el("div");
  btnWrap.style.marginTop = "18px";
  const btn = el("button", "btn btn-primary btn-block", "连接");
  btnWrap.append(btn);
  card.append(btnWrap);
  const foot = el("div", "small muted", "连接仅用于部署与管理，不会留存你的凭证");
  foot.style.textAlign = "center";
  wrap.append(card, foot);
  content.append(wrap);

  btn.onclick = async () => {
    errSlot.innerHTML = "";
    btn.disabled = true;
    btn.textContent = "";
    const spin = el("span", "spin", "◌");
    btn.append(spin, document.createTextNode("连接中…"));
    const uh = splitUserHost();
    const body = {
      host: uh.host,
      port: parseInt(portI.value, 10) || 22,
      user: uh.user,
      password: pwdI.value,
      key_path: tabKey.className === "on" ? keyI.value.trim() : "",
    };
    try {
      await api("/api/connect", { method: "POST", body: body });
      S.connStr = uh.user + "@" + uh.host;
      S.connHost = uh.host;
      S.pwdAuth = pwdI.value !== ""; // 密码登录才可能有登录密码供 sudo 回退
      setBadge(true, "已连接 " + S.connStr);
      location.hash = "#/probe";
    } catch (e) {
      errSlot.innerHTML = "";
      errSlot.append(alertBar("err", "✗ " + e.message));
      btn.disabled = false;
      btn.textContent = "连接";
    }
  };
}

/* ================== 环境探测页 ================== */
function probeItem(kind, icon, title, desc) {
  const item = el("div", "probe-item");
  item.append(el("span", "ic " + kind, icon));
  const tx = el("div");
  tx.append(el("div", "t", title));
  if (desc) tx.append(el("div", "d", desc));
  item.append(tx);
  return item;
}

function pageProbe(content) {
  content.append(el("h2", "pagehead", "环境探测"));
  const sub = el("div", "pagesub");
  sub.append(document.createTextNode("已连接 "));
  sub.append(el("b", "", S.connStr || "（未知）"));
  sub.append(document.createTextNode(" · "));
  const again = el("a", "", "重新探测");
  again.href = "javascript:void(0)";
  again.onclick = () => route();
  sub.append(again);
  content.append(sub);
  const slot = el("div");
  content.append(slot);
  slot.append(el("div", "muted", "探测中，请稍候…"));

  api("/api/probe").then((p) => {
    S.probe = p;
    slot.innerHTML = "";
    renderProbeResult(slot, p);
  }).catch((e) => {
    slot.innerHTML = "";
    slot.append(alertBar("err", "✗ 探测失败：" + e.message));
    const retry = el("button", "btn btn-primary", "重试");
    retry.onclick = () => route();
    slot.append(retry);
  });
}

function renderProbeResult(slot, p) {
  const items = el("div");
  // Docker / Compose
  if (p.docker_ok) items.append(probeItem("ok", "✓", "Docker " + (p.docker_version || ""), ""));
  else if (p.docker_cli) items.append(probeItem("err", "✗", "Docker 已安装但当前用户无法访问", p.docker_detail || ""));
  else items.append(probeItem("err", "✗", "未检测到 Docker", p.docker_detail || "docker 命令不存在"));
  if (p.compose_ok) items.append(probeItem("ok", "✓", "Compose 插件可用", ""));
  else items.append(probeItem("err", "✗", "未检测到 compose 插件", "docker compose 插件缺失"));
  if (p.need_sudo) items.append(probeItem("info", "ℹ", "Docker 命令将以 sudo 执行", "已验证通过；密码仅存内存，不落盘"));
  // 端口：已有部署时用 info 中性提示，否则 warn 并预告自动避让
  if (p.port_gitea_busy) {
    if (p.existing) items.append(probeItem("info", "ℹ", "端口 3000 已被占用", "容器 " + p.port_gitea_busy + "（本部署的 Gitea）"));
    else items.append(probeItem("warn", "⚠", "端口 3000 已被占用", "容器 " + p.port_gitea_busy + "，部署时将自动避让到 3001"));
  } else {
    items.append(probeItem("ok", "✓", "端口 3000 空闲", "将用于 Gitea Web"));
  }
  if (p.port_ok_busy) {
    if (p.existing) items.append(probeItem("info", "ℹ", "端口 3100 已被占用", "容器 " + p.port_ok_busy + "（本部署的 okserver）"));
    else items.append(probeItem("warn", "⚠", "端口 3100 已被占用", "容器 " + p.port_ok_busy + "，部署时将自动避让到 3101"));
  } else {
    items.append(probeItem("ok", "✓", "端口 3100 空闲", "将用于 okserver API"));
  }
  if (p.gitea_found) items.append(probeItem("info", "ℹ", "检测到已有 Gitea", p.gitea_detail || ""));

  // 已有部署：直接进入管理模式
  if (p.existing) {
    const note = alertBar("ok", "");
    note.textContent = "";
    note.append(document.createTextNode("✓ 检测到已有部署："));
    const dirSpan = el("span", "mono", p.deploy_dir || "");
    note.append(dirSpan);
    slot.append(note, items);
    S.deployDir = p.deploy_dir;
    const bottom = el("div");
    bottom.style.marginTop = "18px";
    const btn = el("button", "btn btn-primary btn-block", "进入管理模式");
    btn.style.padding = "12px";
    btn.onclick = () => { location.hash = "#/manage"; };
    bottom.append(btn);
    slot.append(bottom);
    return;
  }
  // 无 Docker / compose：红色终止块 + 指引（按真实原因分流：装 Docker / 加 docker 组）
  if (!p.docker_ok || !p.compose_ok) {
    slot.append(items);
    const block = el("div", "pcard card-danger");
    block.style.borderWidth = "2px";
    const permDenied = p.docker_cli && !p.docker_ok && (p.docker_detail || "").toLowerCase().indexOf("permission denied") >= 0;
    const h = el("h3", "", "✗ 无法继续部署");
    h.style.color = "var(--danger)";
    const d = el("div", "pdesc");
    d.style.color = "var(--danger)";
    const ul = el("ul", "small");
    ul.style.cssText = "margin:0;padding-left:20px;line-height:2";
    if (permDenied) {
      d.textContent = "Docker 已安装，但当前用户无权访问 daemon。okdeploy 可以直接用 sudo 执行部署（密码仅存内存，不落盘）：";
      const row = el("div");
      row.style.cssText = "display:flex;gap:8px;align-items:center;margin:10px 0;flex-wrap:wrap";
      const sudoPwI = el("input", "pinput");
      sudoPwI.type = "password";
      sudoPwI.placeholder = S.pwdAuth ? "sudo 密码（默认同登录密码）" : "sudo 密码（私钥登录必填）";
      sudoPwI.style.width = "240px";
      const sudoBtn = el("button", "btn btn-primary", "启用 sudo 并重新探测");
      sudoBtn.onclick = async () => {
        sudoBtn.disabled = true;
        sudoBtn.textContent = "验证中…";
        try {
          await api("/api/enable-sudo", { method: "POST", body: { password: sudoPwI.value } });
          route(); // 重新探测
        } catch (e2) {
          block.append(alertBar("err", "✗ " + e2.message));
          sudoBtn.disabled = false;
          sudoBtn.textContent = "启用 sudo 并重新探测";
        }
      };
      row.append(sudoPwI, sudoBtn);
      const manual = el("div", "small muted");
      manual.textContent = "或者手动处理：在 NAS 上执行 sudo usermod -aG docker " + (S.connStr || "").split("@")[0] + "，重新登录 SSH 后点「重新探测」";
      block.append(h, d, row, manual);
      slot.append(block);
      return;
    }
    d.textContent = "okdeploy 需要 NAS 已安装 Docker 与 compose 插件，请先安装后重新探测：";
    const li1 = el("li");
    li1.append(el("b", "", "群晖 Synology"), document.createTextNode("：套件中心安装「Container Manager」（DSM 7.2+）"));
    const li2 = el("li");
    li2.append(el("b", "", "威联通 QNAP"), document.createTextNode("：App Center 安装「Container Station」"));
    const li3 = el("li");
    li3.append(el("b", "", "其他 Linux NAS"), document.createTextNode("：安装 Docker Engine 24+ 及 docker compose 插件"));
    ul.append(li1, li2, li3);
    block.append(h, d, ul);
    slot.append(block);
    return;
  }
  // 已有 Gitea：分支选择两张卡
  if (p.gitea_found) {
    slot.append(items);
    const wrap = el("div", "branch-wrap");
    const c1 = el("div", "branch-card");
    const h41 = el("h4", "", "📦 全新部署");
    const giteaPort = p.port_gitea_busy ? 3001 : 3000;
    const p1 = el("p", "", "部署 okserver + Gitea 双容器。新 Gitea 使用 " + giteaPort + " 端口，与已有 Gitea 互不干扰。适合想独立管理知识库仓库的场景。");
    c1.append(h41, p1);
    c1.onclick = () => { location.hash = "#/deploy?mode=full"; };
    const c2 = el("div", "branch-card");
    const h42 = el("h4", "", "🔗 接入已有 Gitea");
    const p2 = el("p", "", "仅部署 okserver 单容器，复用已有 Gitea 作为 Git 后端。需要提供管理员 token，并完成治理项确认。适合已有统一 Git 服务的场景。");
    c2.append(h42, p2);
    c2.onclick = () => { location.hash = "#/deploy?mode=external"; };
    wrap.append(c1, c2);
    slot.append(wrap);
    return;
  }
  // 全新机器：直接开始部署
  slot.append(items);
  const bottom = el("div");
  bottom.style.marginTop = "18px";
  const btn = el("button", "btn btn-primary btn-block", "开始部署");
  btn.style.padding = "12px";
  btn.onclick = () => { location.hash = "#/deploy"; };
  bottom.append(btn);
  slot.append(bottom);
}

/* ================== 部署页 ================== */
function pageDeploy(content, query) {
  const mode = (new URLSearchParams(query).get("mode")) || "full";
  const p = S.probe || {};
  const giteaPortDef = p.port_gitea_busy ? "3001" : "3000";
  const okPortDef = p.port_ok_busy ? "3101" : "3100";

  if (mode === "external") {
    content.append(el("h2", "pagehead", "接入已有 Gitea"));
    content.append(el("div", "pagesub", "仅部署 okserver 单容器，复用已有 Gitea" + (p.gitea_detail ? "（" + p.gitea_detail + "）" : "") + "作为 Git 后端"));
  } else {
    content.append(el("h2", "pagehead", "全新部署"));
    content.append(el("div", "pagesub", "将在 NAS 上创建 okserver + Gitea 双容器（docker compose）"));
  }

  const errSlot = el("div");
  content.append(errSlot);
  const card = el("div", "pcard");
  // 部署目录（原样传给远端，~ 由远端 sh 展开，前端不做本地展开）
  const dirI = pinput("mono", "~/openknowledge", "230px");
  const browse = el("button", "btn", "浏览…");
  browse.onclick = () => openDirPicker(dirI);
  card.append(prow("部署目录", [dirI, browse]));
  let giteaPortI = null;
  if (mode === "full") {
    giteaPortI = pinput("mono", giteaPortDef, "90px");
    card.append(prow("Gitea 端口", [giteaPortI]));
    if (p.port_gitea_busy) {
      const w = el("div", "fb-warn", "⚠ 3000 被占用，已自动避让");
      w.style.margin = "-4px 0 4px 120px";
      card.append(w);
    }
  }
  const okPortI = pinput("mono", okPortDef, "90px");
  card.append(prow("okserver 端口", [okPortI]));
  if (p.port_ok_busy) {
    const w = el("div", "fb-warn", "⚠ 3100 被占用，已自动避让");
    w.style.margin = "-4px 0 4px 120px";
    card.append(w);
  }
  const tagI = pinput("mono", "latest", "140px");
  card.append(prow("镜像版本", [tagI, el("span", "muted small", "z7dream/openknowledge-okserver:latest")]));
  let giteaUrlI = null, tokenI = null, smokeFb = null;
  if (mode === "external") {
    giteaUrlI = pinput("mono", S.connHost ? "http://" + S.connHost + ":3000" : "", "230px");
    if (!S.connHost) giteaUrlI.placeholder = "http://192.168.1.10:3000";
    card.append(prow("已有 Gitea 地址", [giteaUrlI]));
    tokenI = pinput("", "", "230px", "password");
    tokenI.placeholder = "gitea admin token";
    const smokeBtn = el("button", "btn", "测试 Gitea");
    card.append(prow("管理员 token", [tokenI, smokeBtn]));
    smokeFb = el("div");
    smokeFb.style.margin = "-4px 0 4px 120px";
    card.append(smokeFb);
    smokeBtn.onclick = async () => {
      smokeFb.innerHTML = "";
      smokeFb.append(el("span", "small muted", "测试中…"));
      try {
        await api("/api/smoke-external", { method: "POST", body: { gitea_url: giteaUrlI.value.trim(), admin_token: tokenI.value } });
        smokeFb.innerHTML = "";
        smokeFb.append(el("span", "fb-ok", "✓ 兼容性验证通过（API v1）"));
      } catch (e) {
        smokeFb.innerHTML = "";
        smokeFb.append(el("span", "fb-err", "✗ " + e.message));
      }
    };
  }
  const btnWrap = el("div");
  btnWrap.style.marginTop = "18px";
  const deployBtn = el("button", "btn btn-primary btn-block", "开始部署");
  btnWrap.append(deployBtn);
  card.append(btnWrap);
  content.append(card);
  const checklistSlot = el("div");
  content.append(checklistSlot);

  function collectSpec(checklistAck) {
    const spec = {
      mode: mode,
      dir: dirI.value.trim(),
      ok_port: parseInt(okPortI.value, 10) || 3100,
      tag: tagI.value.trim() || "latest",
      checklist_ack: !!checklistAck,
    };
    if (mode === "full") {
      spec.gitea_port = parseInt(giteaPortI.value, 10) || 3000;
      spec.root_url = "http://" + (S.connHost || "") + ":" + spec.gitea_port + "/";
    } else {
      spec.gitea_url = giteaUrlI.value.trim();
      spec.admin_token = tokenI.value;
    }
    return spec;
  }

  async function submit(checklistAck) {
    errSlot.innerHTML = "";
    deployBtn.disabled = true;
    try {
      await api("/api/deploy", { method: "POST", body: collectSpec(checklistAck) });
      runDeployView(content, collectSpec(checklistAck));
    } catch (e) {
      deployBtn.disabled = false;
      // external 未确认治理清单：渲染确认卡，全勾后带 checklist_ack:true 重发
      if (e.payload && e.payload.need_checklist) {
        renderChecklist(checklistSlot, e.payload.need_checklist, () => submit(true));
        return;
      }
      errSlot.append(alertBar("err", "✗ " + e.message));
    }
  }
  deployBtn.onclick = () => submit(false);
}

// 治理清单确认卡（照原型 card-warn 区块；四项全勾后"开始部署"可用）
function renderChecklist(slot, items, onConfirm) {
  slot.innerHTML = "";
  const card = el("div", "pcard card-warn");
  card.style.borderWidth = "2px";
  const h = el("h3", "", "⚠ 接入已有 Gitea 前请逐项确认");
  h.style.color = "var(--warn)";
  card.append(h);
  card.append(el("div", "pdesc", "okdeploy 不会修改你的 Gitea 配置，以下治理项需要你已在 Gitea 中自行设置："));
  const boxes = [];
  for (const text of items) {
    const lab = el("label", "ck");
    const cb = el("input");
    cb.type = "checkbox";
    lab.append(cb, document.createTextNode(text));
    boxes.push(cb);
    card.append(lab);
  }
  const row = el("div", "prow");
  row.style.cssText = "margin-top:14px;margin-bottom:0";
  const btn = el("button", "btn btn-primary", "开始部署");
  btn.disabled = true;
  btn.onclick = onConfirm;
  row.append(btn, el("span", "small muted", "全部确认后可开始"));
  for (const cb of boxes) {
    cb.onchange = () => { btn.disabled = !boxes.every((b) => b.checked); };
  }
  card.append(row);
  slot.append(card);
  card.scrollIntoView({ behavior: "smooth", block: "nearest" });
}

// 部署中视图：sticky 状态条 + 只读摘要 + SSE 日志；完成后显示一次性 root 密码
function runDeployView(content, spec) {
  content.innerHTML = "";
  const stepbar = el("div", "stepbar");
  const st = el("span", "st");
  const spin = el("span", "spin", "◌");
  spin.style.color = "var(--primary)";
  st.append(spin, document.createTextNode("部署中…"));
  const bar = el("div", "bar");
  const barI = el("i");
  barI.style.width = "0%";
  bar.append(barI);
  stepbar.append(st, bar);
  content.append(stepbar);
  content.append(el("h2", "pagehead", "部署中"));
  content.append(el("div", "pagesub", "表单已锁定，部署完成后将显示一次性 root 初始密码"));
  const summary = el("div", "summary");
  summary.append(document.createTextNode("部署目录 "));
  summary.append(el("b", "mono", spec.dir));
  if (spec.mode === "full") {
    summary.append(document.createTextNode(" Gitea 端口 "));
    summary.append(el("b", "mono", String(spec.gitea_port)));
  }
  summary.append(document.createTextNode(" okserver 端口 "));
  summary.append(el("b", "mono", String(spec.ok_port)));
  summary.append(document.createTextNode(" 镜像 "));
  summary.append(el("b", "mono", spec.tag));
  summary.append(el("span", "muted", "（只读）"));
  content.append(summary);
  const logSlot = el("div");
  content.append(logSlot);
  const doneSlot = el("div");
  content.append(doneSlot);
  const logHint = el("div", "small muted", "日志实时滚动，断网中断后可从断点重试");
  logHint.style.marginTop = "8px";
  content.append(logHint);

  const t0 = Date.now(); // 只响应本次任务启动后的日志（SSE 首帧会补历史）
  const seenSteps = new Set();
  let done = false;
  mountLogPane(logSlot, (ev) => {
    const ts = new Date(ev.ts).getTime();
    if (ts && ts < t0 - 1000) return;
    if (ev.step && !seenSteps.has(ev.step)) {
      seenSteps.add(ev.step);
      st.textContent = "";
      const sp = el("span", "spin", "◌");
      sp.style.color = "var(--primary)";
      st.append(sp, document.createTextNode("当前步骤：" + ev.step));
      barI.style.width = Math.min(90, seenSteps.size * 12) + "%";
    }
    if (done) return;
    if (ev.text.indexOf("任务完成：") >= 0) {
      done = true;
      barI.style.width = "100%";
      st.textContent = "✓ 部署完成";
      showDeployDone(doneSlot, spec);
    } else if (ev.text.indexOf("失败：") >= 0) {
      done = true;
      st.textContent = "✗ 部署失败";
      doneSlot.append(alertBar("err", "✗ " + ev.text + "（可修正后重试，已完成步骤会保留）"));
    }
  });
}

// 部署完成块（照原型 variant=done：密码卡 + 下一步指引）
async function showDeployDone(slot, spec) {
  let pw = "";
  try {
    const r = await api("/api/deploy/result");
    pw = r.root_password || "";
  } catch (e) { /* 404=暂无，落到下方提示 */ }
  slot.append(alertBar("ok", "✓ 部署完成：okserver 已在 " + (S.connHost || "NAS") + " 上运行"));
  if (pw) {
    slot.append(pwdCard("root 初始密码", pw, "请立即保存：此密码已在服务器上删除，无法再次查看"));
  } else {
    slot.append(alertBar("err", "未能读取 root 初始密码，请稍后在管理模式中重置"));
  }
  const next = pcard("下一步");
  const ol = el("ol", "steps");
  ol.style.cssText = "margin:8px 0 0;padding-left:20px";
  const li1 = el("li");
  li1.append(document.createTextNode("在客户端 "), el("b", "", "OkManager → 服务器页"), document.createTextNode(" 填服务器地址 "));
  li1.append(el("span", "mono", "http://" + (S.connHost || "<nas>") + ":" + spec.ok_port));
  const li2 = el("li");
  li2.append(document.createTextNode("用 "), el("span", "mono", "root"), document.createTextNode(" + 上方初始密码登录，并按提示修改密码"));
  const li3 = el("li", "", "创建用户和项目仓库，开始使用");
  ol.append(li1, li2, li3);
  next.append(ol);
  slot.append(next);
  const row = el("div", "prow");
  const manage = el("button", "btn btn-primary", "进入管理模式");
  manage.onclick = () => { S.deployDir = spec.dir; location.hash = "#/manage"; };
  const close = el("button", "btn", "关闭窗口");
  close.onclick = () => window.close();
  row.append(manage, close);
  slot.append(row);
}

/* ================== 管理模式页 ================== */
function pageManage(content) {
  content.append(el("h2", "pagehead", "管理模式"));
  const sub = el("div", "pagesub");
  sub.append(document.createTextNode("部署目录 "));
  sub.append(el("span", "mono", S.deployDir || "（未知，请先探测）"));
  sub.append(document.createTextNode(" · "));
  const re = el("a", "", "重新探测");
  re.href = "javascript:void(0)";
  re.onclick = () => { location.hash = "#/probe"; };
  const disc = el("a", "", "断开连接");
  disc.href = "javascript:void(0)";
  disc.onclick = async () => {
    try { await api("/api/disconnect", { method: "POST" }); } catch (e) { /* 忽略 */ }
    setBadge(false, "未连接");
    S.connStr = ""; S.connHost = ""; S.probe = null; S.deployDir = "";
    location.hash = "#/connect";
  };
  sub.append(re, document.createTextNode(" · "), disc);
  content.append(sub);

  if (!S.deployDir) {
    const a = alertBar("err", "尚未确定部署目录：请先在探测页检测已有部署，或完成一次部署");
    content.append(a);
    const btn = el("button", "btn btn-primary", "前往环境探测");
    btn.onclick = () => { location.hash = "#/probe"; };
    content.append(btn);
    return;
  }

  const errSlot = el("div");
  content.append(errSlot);
  const statSlot = el("div");
  content.append(statSlot);
  statSlot.append(el("div", "muted", "状态查询中…"));
  let status = null;

  // ---- 升级 ----
  const upgrade = pcard("升级", "拉取新镜像并重建容器，数据卷不受影响，停机约 10 秒");
  const upRow = el("div", "prow");
  const curTag = el("span", "muted small", "当前 … →");
  const tagI = pinput("mono", "latest", "120px");
  const upBtn = el("button", "btn btn-primary", "升级");
  upRow.append(curTag, tagI, upBtn);
  upgrade.append(upRow);

  // ---- 查看日志 ----
  const viewLogs = pcard("查看日志", "拉取容器最近日志用于排障");
  const lgRow = el("div", "prow");
  const tailSel = el("select", "pselect mono");
  for (const n of ["100", "200", "500", "2000"]) {
    const op = el("option", "", n);
    op.value = n;
    if (n === "200") op.selected = true;
    tailSel.append(op);
  }
  const pullBtn = el("button", "btn", "拉取日志");
  lgRow.append(tailSel, el("span", "muted small", "行"), pullBtn, el("span", "small muted", "结果显示在页面底部日志面板"));
  viewLogs.append(lgRow);

  // ---- 重置 root 密码 ----
  const reset = pcard("重置 root 密码", "root 密码丢失时使用。将在服务器上重新生成 32 位随机密码，旧密码立即失效");
  reset.classList.add("card-danger");
  reset.style.borderWidth = "2px";
  const rsRow = el("div", "prow");
  const rsI = pinput("", "", "180px");
  rsI.placeholder = "输入 RESET 确认";
  const rsBtn = el("button", "btn btn-danger", "重置密码");
  rsBtn.disabled = true;
  rsI.oninput = () => { rsBtn.disabled = rsI.value !== "RESET"; };
  rsRow.append(rsI, rsBtn);
  const rsResult = el("div");
  reset.append(rsRow, rsResult);

  // ---- 备份 ----
  const backup = pcard("备份", "停机数秒打包数据卷并下载到本机");
  const bkRow = el("div", "prow");
  const bkBtn = el("button", "btn", "立即备份");
  bkRow.append(bkBtn);
  backup.append(bkRow);

  // ---- 恢复 ----
  const restore = pcard("恢复");
  restore.classList.add("card-danger");
  restore.style.borderWidth = "2px";
  const rtRow = el("div", "prow");
  const fileI = el("input");
  fileI.type = "file";
  fileI.accept = ".tar";
  fileI.style.display = "none";
  const pickBtn = el("button", "btn", "选择备份文件…");
  const fileName = el("span", "mono small muted", "（未选择）");
  const rtI = pinput("", "", "180px");
  rtI.placeholder = "输入 RESTORE 确认";
  pickBtn.onclick = () => fileI.click();
  // 按钮需同时满足：已选文件 + 确认词 RESTORE（照卸载卡 DELETE 模式）
  const rtCheck = () => { rtBtn.disabled = !(fileI.files.length && rtI.value === "RESTORE"); };
  fileI.onchange = () => { fileName.textContent = fileI.files.length ? fileI.files[0].name : "（未选择）"; rtCheck(); };
  rtI.oninput = rtCheck;
  rtRow.append(fileI, pickBtn, fileName, rtI);
  const rtWarn = el("div", "fb-err", "⚠ 恢复将删除并覆盖服务器现有数据，不可撤销。建议先执行备份。");
  rtWarn.style.margin = "2px 0 8px";
  const rtBtn = el("button", "btn btn-danger", "开始恢复");
  rtBtn.disabled = true;
  restore.append(rtRow, rtWarn, rtBtn);

  // ---- 卸载 ----
  const uninstall = pcard("卸载", "删除 okdeploy 创建的容器与 compose 项目");
  uninstall.classList.add("card-danger");
  uninstall.style.borderWidth = "2px";
  const unRow = el("div", "prow");
  const unI = pinput("", "", "180px");
  unI.placeholder = "输入 DELETE 确认";
  unRow.append(unI);
  const delLab = el("label", "ck");
  const delCb = el("input");
  delCb.type = "checkbox";
  delLab.append(delCb, document.createTextNode("同时删除数据目录（不可恢复）"));
  const unBtnRow = el("div", "prow");
  unBtnRow.style.marginTop = "12px";
  const unBtn = el("button", "btn btn-danger", "卸载");
  unBtn.disabled = true;
  unI.oninput = () => { unBtn.disabled = unI.value !== "DELETE"; };
  unBtnRow.append(unBtn);
  uninstall.append(unRow, delLab, unBtnRow);

  // ---- 操作日志区（SSE）与底部容器日志面板（一次性拉取）----
  const opsSlot = el("div");
  const foldSlot = el("div");
  foldSlot.style.marginTop = "12px";
  renderFold(foldSlot, null, 0);
  content.append(upgrade, viewLogs, reset, backup, restore, uninstall, opsSlot, foldSlot);

  // 状态查询
  api("/api/status?dir=" + encodeURIComponent(S.deployDir)).then((st) => {
    status = st;
    statSlot.innerHTML = "";
    const row = el("div", "stat3");
    for (const c of st.containers || []) {
      const card = el("div", "pcard");
      card.append(el("div", "cap", c.name + " 容器"));
      const val = el("div", "val");
      const dot = el("span", "dot");
      const up = /^Up/i.test(c.status || "");
      dot.style.background = up ? "var(--ok)" : "var(--danger)";
      val.append(dot, document.createTextNode(up ? "运行中 · " + c.status : (c.status || "未知")));
      card.append(val);
      row.append(card);
    }
    const info = el("div", "pcard");
    info.append(el("div", "cap", "镜像版本 / 数据占用"));
    const val = el("div", "val mono", (st.image || "未知") + " · " + (st.disk_usage || "?"));
    val.style.fontSize = "12.5px";
    info.append(val);
    row.append(info);
    statSlot.append(row);
    const tag = (st.image || "").split(":").pop();
    if (tag) { curTag.textContent = ""; curTag.append(document.createTextNode("当前 ")); const m = el("span", "mono", tag); curTag.append(m, document.createTextNode(" →")); tagI.value = tag; }
  }).catch((e) => {
    statSlot.innerHTML = "";
    errSlot.append(alertBar("err", "✗ 状态查询失败：" + e.message));
  });

  // 操作通用：起任务 → SSE 日志 → 完成/失败提示。onDone(ev) 返回 true 表示已处理完成。
  function watchOps(t0, onDone) {
    let done = false;
    mountLogPane(opsSlot, (ev) => {
      const ts = new Date(ev.ts).getTime();
      if (ts && ts < t0 - 1000) return;
      if (done) return;
      if (ev.text.indexOf("任务完成：") >= 0) { done = true; onDone(null); }
      else if (ev.text.indexOf("失败：") >= 0) { done = true; onDone(new Error(ev.text)); }
    }, "h160");
  }

  upBtn.onclick = async () => {
    errSlot.innerHTML = "";
    const t0 = Date.now();
    try {
      await api("/api/upgrade", { method: "POST", body: { dir: S.deployDir, tag: tagI.value.trim() } });
      watchOps(t0, (err) => {
        if (err) errSlot.append(alertBar("err", "✗ " + err.message));
        else errSlot.append(alertBar("ok", "✓ 升级完成"));
      });
    } catch (e) { errSlot.append(alertBar("err", "✗ " + e.message)); }
  };

  pullBtn.onclick = async () => {
    renderFold(foldSlot, null, 0, "拉取中…");
    try {
      const r = await api("/api/remote-logs?dir=" + encodeURIComponent(S.deployDir) + "&tail=" + tailSel.value);
      renderFold(foldSlot, r.logs || "", tailSel.value);
    } catch (e) {
      renderFold(foldSlot, null, 0, "✗ " + e.message);
    }
  };

  rsBtn.onclick = async () => {
    errSlot.innerHTML = "";
    rsResult.innerHTML = "";
    rsBtn.disabled = true;
    const t0 = Date.now();
    try {
      await api("/api/reset-root", { method: "POST", body: { dir: S.deployDir, confirm: rsI.value } });
      watchOps(t0, async (err) => {
        if (err) { rsResult.append(alertBar("err", "✗ " + err.message)); return; }
        try {
          const r = await api("/api/deploy/result");
          rsResult.append(pwdCard("新 root 密码", r.root_password, "请立即保存：旧密码已失效，此密码不会再次显示"));
        } catch (e2) {
          rsResult.append(alertBar("err", "重置完成但未能读取新密码：" + e2.message));
        }
      });
    } catch (e) { errSlot.append(alertBar("err", "✗ " + e.message)); }
  };

  bkBtn.onclick = () => {
    // 浏览器原生下载（EventSource/下载无法设请求头，token 走 query，后端仅绑 127.0.0.1）
    window.location = "/api/backup?dir=" + encodeURIComponent(S.deployDir) + "&token=" + encodeURIComponent(window.OK_TOKEN);
    watchOps(Date.now(), () => {});
  };

  rtBtn.onclick = async () => {
    errSlot.innerHTML = "";
    if (!fileI.files.length) return;
    rtBtn.disabled = true;
    const t0 = Date.now();
    const fd = new FormData();
    fd.append("file", fileI.files[0]);
    fd.append("dir", S.deployDir);
    fd.append("confirm", rtI.value);
    try {
      // multipart 不能走 api() 的 JSON 通道，原生 fetch + X-Ok-Token 头
      const r = await fetch("/api/restore", { method: "POST", headers: { "X-Ok-Token": window.OK_TOKEN }, body: fd });
      const data = await r.json().catch(() => null);
      if (!r.ok) throw new Error((data && data.error) || ("HTTP " + r.status));
      watchOps(t0, (err) => {
        if (err) errSlot.append(alertBar("err", "✗ " + err.message));
        else errSlot.append(alertBar("ok", "✓ 恢复完成"));
        rtBtn.disabled = false;
      });
    } catch (e) {
      errSlot.append(alertBar("err", "✗ " + e.message));
      rtBtn.disabled = false;
    }
  };

  unBtn.onclick = async () => {
    errSlot.innerHTML = "";
    unBtn.disabled = true;
    const t0 = Date.now();
    try {
      await api("/api/uninstall", { method: "POST", body: { dir: S.deployDir, confirm: unI.value, delete_data: delCb.checked } });
      watchOps(t0, (err) => {
        if (err) { errSlot.append(alertBar("err", "✗ " + err.message)); unBtn.disabled = false; return; }
        S.deployDir = "";
        errSlot.append(alertBar("ok", "✓ 已卸载，即将返回连接页…"));
        setTimeout(() => { location.hash = "#/connect"; }, 1200);
      });
    } catch (e) { errSlot.append(alertBar("err", "✗ " + e.message)); unBtn.disabled = false; }
  };
}

// 底部容器日志面板（照原型 logfold 折叠块 + .log h160，非 SSE，一次性拉取）
function renderFold(slot, logs, tail, hint) {
  slot.innerHTML = "";
  const open = logs !== null;
  const title = logs !== null ? "容器日志（最近 " + tail + " 行）" : "容器日志";
  const head = el("div", "logfold", (open ? "▾ " : "▸ ") + title + (open ? "（点击收起）" : "（点击展开）"));
  head.onclick = () => {
    if (logs !== null) renderFold(slot, null, 0);
  };
  slot.append(head);
  if (open) {
    const body = el("div", "log h160");
    body.style.borderRadius = "0 0 8px 8px";
    body.textContent = logs || "（无日志）";
    slot.append(body);
  } else if (hint) {
    const body = el("div", "log h160");
    body.style.borderRadius = "0 0 8px 8px";
    body.textContent = hint;
    slot.append(body);
  }
}

route();
