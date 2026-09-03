// okdeploy 前端：hash 路由 + SSE 日志。token 经 #token= fragment 进入（同 OkManager 模式）。
// 视觉事实源：docs/prototypes/prototype-okdeploy-final.html（gitignored，本地评审产物）
"use strict";

/* ================= i18n（仅界面文案；后端返回的错误消息与日志行原文透传，不翻译） ================= */
const I18N = {
  zh: {
    pageTitle: "Okryptos 服务端部署",
    badgeConnected: "已连接 ", badgeDisconnected: "未连接",
    downloadLog: "下载日志",
    onceOnly: "（只显示这一次）", copy: "复制", copied: "已复制", copyFail: "复制失败，请手动选择",
    pickDirTitle: "选择部署目录", currentPrefix: "当前：", pickThisDir: "选此目录", cancel: "取消", noSubdirs: "（无子目录）",
    connectTitle: "连接到 NAS", connectSub: "通过 SSH 部署 Okryptos 服务端",
    connectedGo: "已连接 {c}，直接进入环境探测 →",
    hostPh: "<user>@<ip> 或 <ip>", sshAddr: "SSH 地址", port: "端口",
    username: "用户名", userPh: "地址里写了 user@ 可留空",
    authMethod: "认证方式", password: "密码", privateKey: "私钥", keyFile: "私钥文件",
    keyHint: "支持 OpenSSH 格式，passphrase 将在连接时询问",
    connect: "连接", connecting: "连接中…",
    connectFoot: "连接仅用于部署与管理，不会留存你的凭证",
    probeTitle: "环境探测", connectedPrefix: "已连接 ", unknownConn: "（未知）",
    reprobe: "重新探测", backToConnect: "返回连接页", probing: "探测中，请稍候…",
    probeFail: "✗ 探测失败：", retry: "重试",
    dockerNoAccess: "Docker 已安装但当前用户无法访问", dockerMissing: "未检测到 Docker", dockerCmdMissing: "docker 命令不存在",
    composeOk: "Compose 插件可用", composeMissing: "未检测到 compose 插件", composePluginMissing: "docker compose 插件缺失",
    dockerSudo: "Docker 命令将以 sudo 执行", sudoVerified: "已验证通过；密码仅存内存，不落盘",
    portBusy: "端口 {p} 已被占用", portBusyOwn: "容器 {c}（本部署的 {what}）",
    portBusyAvoid: "容器 {c}，部署时将自动避让到 {p2}",
    portFree: "端口 {p} 空闲", portGiteaUse: "将用于 Gitea Web", portOkUse: "将用于 okserver API",
    giteaFound: "检测到已有 Gitea", existingFound: "✓ 检测到已有部署：", enterManage: "进入管理模式",
    cannotDeploy: "✗ 无法继续部署",
    dockerPermDesc: "Docker 已安装，但当前用户无权访问 daemon。okdeploy 可以直接用 sudo 执行部署（密码仅存内存，不落盘）：",
    sudoPwPhSame: "sudo 密码（默认同登录密码）", sudoPwPhKey: "sudo 密码（私钥登录必填）",
    enableSudo: "启用 sudo 并重新探测", verifying: "验证中…",
    manualFix: "或者手动处理：在 NAS 上执行 sudo usermod -aG docker {u}，重新登录 SSH 后点「重新探测」",
    needDockerDesc: "okdeploy 需要 NAS 已安装 Docker 与 compose 插件，请先安装后重新探测：",
    nasSyno: "群晖 Synology", nasQnap: "威联通 QNAP", nasOther: "其他 Linux NAS",
    synoHint: "：套件中心安装「Container Manager」（DSM 7.2+）",
    qnapHint: "：App Center 安装「Container Station」",
    otherHint: "：安装 Docker Engine 24+ 及 docker compose 插件",
    fullDeployTitle: "📦 全新部署",
    fullDeployDesc: "部署 okserver + Gitea 双容器。新 Gitea 使用 {p} 端口，与已有 Gitea 互不干扰。适合想独立管理知识库仓库的场景。",
    extTitle: "🔗 接入已有 Gitea", extDeployHead: "接入已有 Gitea",
    extDesc: "仅部署 okserver 单容器，复用已有 Gitea 作为 Git 后端。需要提供管理员 token，并完成治理项确认。适合已有统一 Git 服务的场景。",
    startDeploy: "开始部署",
    extDeploySub: "仅部署 okserver 单容器，复用已有 Gitea{d}作为 Git 后端", detailParens: "（{d}）",
    fullDeployHead: "全新部署", fullDeploySub: "将在 NAS 上创建 okserver + Gitea 双容器（docker compose）",
    backToProbe: "← 返回探测页",
    browse: "浏览…", deployDir: "部署目录", giteaPort: "Gitea 端口", portAvoided: "⚠ {p} 被占用，已自动避让",
    okPort: "okserver 端口", imageTag: "镜像版本",
    giteaUrl: "已有 Gitea 地址", testGitea: "测试 Gitea", adminToken: "管理员 token",
    testing: "测试中…", smokeOk: "✓ 兼容性验证通过（API v1）",
    checklistTitle: "⚠ 接入已有 Gitea 前请逐项确认",
    checklistDesc: "okdeploy 不会修改你的 Gitea 配置，以下治理项需要你已在 Gitea 中自行设置：",
    checklistHint: "全部确认后可开始",
    deploying: "部署中…", deployHead: "部署中",
    deploySub: "表单已锁定，部署完成后将显示一次性 root 初始密码",
    sumDir: "部署目录 ", sumGiteaPort: " Gitea 端口 ", sumOkPort: " okserver 端口 ", sumImage: " 镜像 ", readonly: "（只读）",
    logHint: "日志实时滚动，断网中断后可从断点重试", currentStep: "当前步骤：",
    deployDone: "✓ 部署完成", deployFailed: "✗ 部署失败", deployFailSuffix: "（可修正后重试，已完成步骤会保留）",
    backToEdit: "← 返回修改配置",
    doneRunning: "✓ 部署完成：okserver 已在 {h} 上运行",
    rootPwTitle: "root 初始密码", rootPwWarn: "请立即保存：此密码已在服务器上删除，无法再次查看",
    rootPwFail: "未能读取 root 初始密码，请稍后在管理模式中重置",
    nextSteps: "下一步",
    nextLi1a: "在客户端 ", nextLi1b: "OkManager → 服务器页", nextLi1c: " 填服务器地址 ",
    nextLi2a: "用 ", nextLi2b: " + 上方初始密码登录，并按提示修改密码",
    nextLi3: "创建用户和项目仓库，开始使用",
    closeWindow: "关闭窗口",
    manageTitle: "管理模式", deployDirPrefix: "部署目录 ", unknownProbe: "（未知，请先探测）",
    disconnect: "断开连接",
    noDirWarn: "尚未确定部署目录：请先在探测页检测已有部署，或完成一次部署",
    goProbe: "前往环境探测", statusLoading: "状态查询中…",
    upgrade: "升级", upgradeDesc: "拉取新镜像并重建容器，数据卷不受影响，停机约 10 秒",
    curPrefix: "当前 ", repull: "重拉镜像", newVersion: "🔔 有新版本 ", upToDate: "✓ 已是最新",
    viewLogs: "查看日志", viewLogsDesc: "拉取容器最近日志用于排障",
    linesUnit: "行", pullLogs: "拉取日志", pullHint: "结果显示在页面底部日志面板",
    resetTitle: "重置 root 密码",
    resetDesc: "root 密码丢失时使用。将在服务器上重新生成 32 位随机密码，旧密码立即失效",
    resetPh: "输入 RESET 确认", resetBtn: "重置密码",
    backupTitle: "备份", backupDesc: "停机数秒打包数据卷并下载到本机", backupNow: "立即备份",
    restoreTitle: "恢复", pickFile: "选择备份文件…", noneSelected: "（未选择）",
    restorePh: "输入 RESTORE 确认",
    restoreWarn: "⚠ 恢复将删除并覆盖服务器现有数据，不可撤销。建议先执行备份。",
    restoreBtn: "开始恢复",
    uninstallTitle: "卸载", uninstallDesc: "删除 okdeploy 创建的容器与 compose 项目",
    deletePh: "输入 DELETE 确认", deleteData: "同时删除数据目录（不可恢复）",
    containerSuffix: " 容器", runningPrefix: "运行中 · ", statusUnknown: "未知",
    imageDiskCap: "镜像版本 / 数据占用", runningVer: "（运行 {v}）",
    statusFail: "✗ 状态查询失败：", upgradeDone: "✓ 升级完成", pulling: "拉取中…",
    newRootPw: "新 root 密码", newRootPwWarn: "请立即保存：旧密码已失效，此密码不会再次显示",
    resetReadFail: "重置完成但未能读取新密码：",
    restoreDone: "✓ 恢复完成", uninstalled: "✓ 已卸载，即将返回连接页…",
    foldTitleTail: "容器日志（最近 {n} 行）", foldTitle: "容器日志",
    foldClose: "（点击收起）", foldOpen: "（点击展开）", noLogs: "（无日志）",
  },
  en: {
    pageTitle: "Okryptos Server Deployment",
    badgeConnected: "Connected to ", badgeDisconnected: "Disconnected",
    downloadLog: "Download log",
    onceOnly: " (shown only once)", copy: "Copy", copied: "Copied", copyFail: "Copy failed; select it manually",
    pickDirTitle: "Choose deploy directory", currentPrefix: "Current: ", pickThisDir: "Use this directory", cancel: "Cancel", noSubdirs: "(no subdirectories)",
    connectTitle: "Connect to NAS", connectSub: "Deploy the Okryptos server over SSH",
    connectedGo: "Connected to {c}, go straight to environment probe →",
    hostPh: "<user>@<ip> or <ip>", sshAddr: "SSH address", port: "Port",
    username: "Username", userPh: "Optional if the address includes user@",
    authMethod: "Auth method", password: "Password", privateKey: "Private key", keyFile: "Key file",
    keyHint: "OpenSSH format; passphrase will be asked on connect",
    connect: "Connect", connecting: "Connecting…",
    connectFoot: "The connection is used only for deploy & manage; your credentials are never stored",
    probeTitle: "Environment Probe", connectedPrefix: "Connected to ", unknownConn: "(unknown)",
    reprobe: "Re-probe", backToConnect: "Back to connect page", probing: "Probing, please wait…",
    probeFail: "✗ Probe failed: ", retry: "Retry",
    dockerNoAccess: "Docker is installed but not accessible by the current user", dockerMissing: "Docker not detected", dockerCmdMissing: "docker command not found",
    composeOk: "Compose plugin available", composeMissing: "Compose plugin not detected", composePluginMissing: "docker compose plugin missing",
    dockerSudo: "Docker commands will run via sudo", sudoVerified: "Verified; the password stays in memory only, never on disk",
    portBusy: "Port {p} is busy", portBusyOwn: "Container {c} (the {what} of this deployment)",
    portBusyAvoid: "Container {c}; will auto-switch to {p2} on deploy",
    portFree: "Port {p} is free", portGiteaUse: "Will serve Gitea Web", portOkUse: "Will serve okserver API",
    giteaFound: "Existing Gitea detected", existingFound: "✓ Existing deployment detected: ", enterManage: "Enter manage mode",
    cannotDeploy: "✗ Cannot continue deployment",
    dockerPermDesc: "Docker is installed, but the current user cannot access the daemon. okdeploy can deploy via sudo (password stays in memory only):",
    sudoPwPhSame: "sudo password (defaults to login password)", sudoPwPhKey: "sudo password (required for key login)",
    enableSudo: "Enable sudo and re-probe", verifying: "Verifying…",
    manualFix: "Or fix it manually: run sudo usermod -aG docker {u} on the NAS, log back in over SSH, then click Re-probe",
    needDockerDesc: "okdeploy requires Docker and the compose plugin on the NAS. Install them, then re-probe:",
    nasSyno: "Synology", nasQnap: "QNAP", nasOther: "Other Linux NAS",
    synoHint: ": install Container Manager from Package Center (DSM 7.2+)",
    qnapHint: ": install Container Station from App Center",
    otherHint: ": install Docker Engine 24+ and the docker compose plugin",
    fullDeployTitle: "📦 Fresh deployment",
    fullDeployDesc: "Deploys okserver + Gitea containers. The new Gitea uses port {p} and won't interfere with the existing one. Best if you want to manage knowledge repos independently.",
    extTitle: "🔗 Use existing Gitea", extDeployHead: "Use existing Gitea",
    extDesc: "Deploys only okserver, reusing the existing Gitea as the Git backend. Requires an admin token and governance checklist confirmation. Best if you already run a unified Git service.",
    startDeploy: "Start deployment",
    extDeploySub: "Deploy only okserver, reusing the existing Gitea{d} as the Git backend", detailParens: " ({d})",
    fullDeployHead: "Fresh deployment", fullDeploySub: "Will create okserver + Gitea containers on the NAS (docker compose)",
    backToProbe: "← Back to probe page",
    browse: "Browse…", deployDir: "Deploy directory", giteaPort: "Gitea port", portAvoided: "⚠ {p} is busy; auto-switched",
    okPort: "okserver port", imageTag: "Image tag",
    giteaUrl: "Existing Gitea URL", testGitea: "Test Gitea", adminToken: "Admin token",
    testing: "Testing…", smokeOk: "✓ Compatibility verified (API v1)",
    checklistTitle: "⚠ Confirm each item before using an existing Gitea",
    checklistDesc: "okdeploy won't change your Gitea config; the following governance items must already be set up in Gitea:",
    checklistHint: "Enabled once all are checked",
    deploying: "Deploying…", deployHead: "Deploying",
    deploySub: "The form is locked; a one-time root password will be shown when done",
    sumDir: "Deploy directory ", sumGiteaPort: " Gitea port ", sumOkPort: " okserver port ", sumImage: " Image ", readonly: "(read-only)",
    logHint: "Logs stream live; if interrupted, you can resume from where it stopped", currentStep: "Current step: ",
    deployDone: "✓ Deployment complete", deployFailed: "✗ Deployment failed", deployFailSuffix: " (fix and retry; completed steps are kept)",
    backToEdit: "← Back to edit config",
    doneRunning: "✓ Deployment complete: okserver is running on {h}",
    rootPwTitle: "Initial root password", rootPwWarn: "Save it now: it has been deleted on the server and cannot be shown again",
    rootPwFail: "Could not read the initial root password; reset it later in manage mode",
    nextSteps: "Next steps",
    nextLi1a: "In the client ", nextLi1b: "OkManager → Server page", nextLi1c: " enter the server address ",
    nextLi2a: "Log in with ", nextLi2b: " + the initial password above, and change it as prompted",
    nextLi3: "Create users and project repos, and start using it",
    closeWindow: "Close window",
    manageTitle: "Manage Mode", deployDirPrefix: "Deploy directory ", unknownProbe: "(unknown, probe first)",
    disconnect: "Disconnect",
    noDirWarn: "Deploy directory not set: probe for an existing deployment first, or finish a deployment",
    goProbe: "Go to environment probe", statusLoading: "Loading status…",
    upgrade: "Upgrade", upgradeDesc: "Pulls the new image and recreates containers; volumes are untouched, ~10s downtime",
    curPrefix: "Current ", repull: "Re-pull image", newVersion: "🔔 New version ", upToDate: "✓ Up to date",
    viewLogs: "View logs", viewLogsDesc: "Pull recent container logs for troubleshooting",
    linesUnit: "lines", pullLogs: "Pull logs", pullHint: "Results appear in the log panel at the bottom",
    resetTitle: "Reset root password",
    resetDesc: "Use when the root password is lost. Generates a new 32-char random password on the server; the old one expires immediately",
    resetPh: "Type RESET to confirm", resetBtn: "Reset password",
    backupTitle: "Backup", backupDesc: "Packs the data volumes and downloads them here; a few seconds of downtime", backupNow: "Back up now",
    restoreTitle: "Restore", pickFile: "Choose backup file…", noneSelected: "(none)",
    restorePh: "Type RESTORE to confirm",
    restoreWarn: "⚠ Restoring deletes and overwrites existing server data; irreversible. Back up first.",
    restoreBtn: "Start restore",
    uninstallTitle: "Uninstall", uninstallDesc: "Removes the containers and compose project created by okdeploy",
    deletePh: "Type DELETE to confirm", deleteData: "Also delete the data directory (irreversible)",
    containerSuffix: " container", runningPrefix: "Running · ", statusUnknown: "Unknown",
    imageDiskCap: "Image / Disk usage", runningVer: " (running {v})",
    statusFail: "✗ Status query failed: ", upgradeDone: "✓ Upgrade complete", pulling: "Pulling…",
    newRootPw: "New root password", newRootPwWarn: "Save it now: the old password is expired and this one won't be shown again",
    resetReadFail: "Reset done but failed to read the new password: ",
    restoreDone: "✓ Restore complete", uninstalled: "✓ Uninstalled; returning to the connect page…",
    foldTitleTail: "Container logs (last {n} lines)", foldTitle: "Container logs",
    foldClose: " (click to collapse)", foldOpen: " (click to expand)", noLogs: "(no logs)",
  },
};
let LANG = "zh";
try { LANG = localStorage.getItem("okdeploy_lang") === "en" ? "en" : "zh"; } catch (e) {}
// 取文案：当前语言缺失回退 zh，再缺回退 key 本身；vars 做 {x} 插值
function t(key, vars) {
  let s = (I18N[LANG] && I18N[LANG][key]) || I18N.zh[key] || key;
  if (vars) { for (const k in vars) s = s.split("{" + k + "}").join(String(vars[k])); }
  return s;
}
// 徽标按当前语言与连接态重刷（语言切换后调用）
function refreshBadge() {
  if (S.connStr) setBadge(true, t("badgeConnected") + S.connStr);
  else setBadge(false, t("badgeDisconnected"));
}
// 切换语言：写 localStorage → 同步 title/<html lang>/胶囊高亮/徽标 → 重渲染当前页
function setLang(l, skipRoute) {
  LANG = l;
  try { localStorage.setItem("okdeploy_lang", l); } catch (e) {}
  document.documentElement.lang = l === "zh" ? "zh-CN" : "en";
  document.title = t("pageTitle");
  document.querySelectorAll("#lang-seg button").forEach((b) => {
    b.className = b.getAttribute("data-lang") === l ? "on" : "";
  });
  refreshBadge();
  if (!skipRoute) route();
}

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
  const dl = el("button", "btn btn-mini", t("downloadLog"));
  const body = el("div", "log " + (heightCls || "h320"));
  dl.onclick = () => downloadLog(body);
  dlw.append(dl);
  wrap.append(dlw, body);
  container.append(wrap);
  if (S.logES) S.logES.close();
  S.logES = new EventSource("/api/logs/stream?token=" + encodeURIComponent(window.OK_TOKEN));
  S.logES.onmessage = (m) => {
    const ev = JSON.parse(m.data);
    // 每行带 HH:MM:SS 时间戳：超时类问题需要对时间轴
    const d = ev.ts ? new Date(ev.ts) : new Date();
    const pad = (n) => String(n).padStart(2, "0");
    const tsEl = el("span", "ts");
    tsEl.textContent = pad(d.getHours()) + ":" + pad(d.getMinutes()) + ":" + pad(d.getSeconds()) + " ";
    body.append(tsEl);
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
  const once = el("span", "muted small", t("onceOnly"));
  once.style.fontWeight = "400";
  head.append(once);
  const pwe = el("div", "pw", pw);
  const cpw = el("div");
  const copyBtn = el("button", "btn", t("copy"));
  copyBtn.onclick = async () => {
    try { await navigator.clipboard.writeText(pw); copyBtn.textContent = t("copied"); }
    catch (e) { copyBtn.textContent = t("copyFail"); }
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
    modal.append(el("h3", "", t("pickDirTitle")));
    const curRow = el("div", "small muted", t("currentPrefix") + (path || "$HOME"));
    curRow.style.marginBottom = "10px";
    modal.append(curRow);
    const list = el("div");
    list.style.cssText = "max-height:220px;overflow-y:auto;margin-bottom:14px";
    modal.append(list);
    const foot = el("div");
    foot.style.cssText = "display:flex;gap:10px;justify-content:flex-end";
    const pick = el("button", "btn btn-primary", t("pickThisDir"));
    pick.onclick = () => { input.value = path || "~/okryptos"; mask.remove(); };
    const cancel = el("button", "btn", t("cancel"));
    cancel.onclick = () => mask.remove();
    foot.append(cancel, pick);
    modal.append(foot);
    let res;
    try { res = await api("/api/ls", { method: "POST", body: { path: path } }); }
    catch (e) { list.append(el("div", "fb-err", e.message)); return; }
    const dirs = res.dirs || [];
    if (!dirs.length) list.append(el("div", "small muted", t("noSubdirs")));
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
  const head = el("h2", "pagehead", t("connectTitle"));
  head.style.cssText = "text-align:center;margin-bottom:2px";
  const sub = el("div", "pagesub", t("connectSub"));
  sub.style.cssText = "text-align:center;margin-bottom:18px";
  card.append(head, sub);
  const errSlot = el("div");
  card.append(errSlot);
  // 从探测/部署页回退回来时会话还在：给快捷入口，不必重连 SSH
  if (S.connStr) {
    const go = el("div", "small");
    go.style.cssText = "text-align:center;margin-bottom:12px";
    const a = el("a", "", t("connectedGo", { c: S.connStr }));
    a.href = "javascript:void(0)";
    a.onclick = () => { location.hash = "#/probe"; };
    go.append(a);
    card.append(go);
  }

  const hostI = pinput("mono", "", "290px");
  hostI.placeholder = t("hostPh");
  const portI = pinput("mono", "22", "56px");
  card.append(prow(t("sshAddr"), [hostI, el("span", "muted small", t("port")), portI]));
  const userI = pinput("", "", "290px");
  userI.placeholder = t("userPh");
  card.append(prow(t("username"), [userI]));

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
  const authRow = prow(t("authMethod"), []);
  authRow.style.marginBottom = "2px";
  card.append(authRow);
  const tabs = el("div", "tabs");
  tabs.style.marginLeft = "0";
  const tabPwd = el("button", "on", t("password"));
  const tabKey = el("button", "", t("privateKey"));
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
      authSlot.append(prow(t("password"), [pwdI]));
    } else {
      authSlot.append(prow(t("keyFile"), [keyI]));
      const hint = el("div", "small muted", t("keyHint"));
      hint.style.margin = "-4px 0 4px 120px";
      authSlot.append(hint);
    }
  }
  tabPwd.onclick = () => showAuth("pwd");
  tabKey.onclick = () => showAuth("key");
  showAuth("pwd");

  const btnWrap = el("div");
  btnWrap.style.marginTop = "18px";
  const btn = el("button", "btn btn-primary btn-block", t("connect"));
  btnWrap.append(btn);
  card.append(btnWrap);
  const foot = el("div", "small muted", t("connectFoot"));
  foot.style.textAlign = "center";
  wrap.append(card, foot);
  content.append(wrap);

  btn.onclick = async () => {
    errSlot.innerHTML = "";
    btn.disabled = true;
    btn.textContent = "";
    const spin = el("span", "spin", "◌");
    btn.append(spin, document.createTextNode(t("connecting")));
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
      setBadge(true, t("badgeConnected") + S.connStr);
      location.hash = "#/probe";
    } catch (e) {
      errSlot.innerHTML = "";
      errSlot.append(alertBar("err", "✗ " + e.message));
      btn.disabled = false;
      btn.textContent = t("connect");
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
  content.append(el("h2", "pagehead", t("probeTitle")));
  const sub = el("div", "pagesub");
  sub.append(document.createTextNode(t("connectedPrefix")));
  sub.append(el("b", "", S.connStr || t("unknownConn")));
  sub.append(document.createTextNode(" · "));
  const again = el("a", "", t("reprobe"));
  again.href = "javascript:void(0)";
  again.onclick = () => route();
  sub.append(again);
  sub.append(document.createTextNode(" · "));
  const back = el("a", "", t("backToConnect"));
  back.href = "javascript:void(0)";
  back.onclick = () => { location.hash = "#/connect"; };
  sub.append(back);
  content.append(sub);
  const slot = el("div");
  content.append(slot);
  slot.append(el("div", "muted", t("probing")));

  api("/api/probe").then((p) => {
    S.probe = p;
    slot.innerHTML = "";
    renderProbeResult(slot, p);
  }).catch((e) => {
    slot.innerHTML = "";
    slot.append(alertBar("err", t("probeFail") + e.message));
    const retry = el("button", "btn btn-primary", t("retry"));
    retry.onclick = () => route();
    slot.append(retry);
  });
}

function renderProbeResult(slot, p) {
  const items = el("div");
  // Docker / Compose
  if (p.docker_ok) items.append(probeItem("ok", "✓", "Docker " + (p.docker_version || ""), ""));
  else if (p.docker_cli) items.append(probeItem("err", "✗", t("dockerNoAccess"), p.docker_detail || ""));
  else items.append(probeItem("err", "✗", t("dockerMissing"), p.docker_detail || t("dockerCmdMissing")));
  if (p.compose_ok) items.append(probeItem("ok", "✓", t("composeOk"), ""));
  else items.append(probeItem("err", "✗", t("composeMissing"), t("composePluginMissing")));
  if (p.need_sudo) items.append(probeItem("info", "ℹ", t("dockerSudo"), t("sudoVerified")));
  // 端口：已有部署时用 info 中性提示，否则 warn 并预告自动避让
  if (p.port_gitea_busy) {
    if (p.existing) items.append(probeItem("info", "ℹ", t("portBusy", { p: 3000 }), t("portBusyOwn", { c: p.port_gitea_busy, what: "Gitea" })));
    else items.append(probeItem("warn", "⚠", t("portBusy", { p: 3000 }), t("portBusyAvoid", { c: p.port_gitea_busy, p2: 3001 })));
  } else {
    items.append(probeItem("ok", "✓", t("portFree", { p: 3000 }), t("portGiteaUse")));
  }
  if (p.port_ok_busy) {
    if (p.existing) items.append(probeItem("info", "ℹ", t("portBusy", { p: 3100 }), t("portBusyOwn", { c: p.port_ok_busy, what: "okserver" })));
    else items.append(probeItem("warn", "⚠", t("portBusy", { p: 3100 }), t("portBusyAvoid", { c: p.port_ok_busy, p2: 3101 })));
  } else {
    items.append(probeItem("ok", "✓", t("portFree", { p: 3100 }), t("portOkUse")));
  }
  if (p.gitea_found) items.append(probeItem("info", "ℹ", t("giteaFound"), p.gitea_detail || ""));

  // 已有部署：直接进入管理模式
  if (p.existing) {
    const note = alertBar("ok", "");
    note.textContent = "";
    note.append(document.createTextNode(t("existingFound")));
    const dirSpan = el("span", "mono", p.deploy_dir || "");
    note.append(dirSpan);
    slot.append(note, items);
    S.deployDir = p.deploy_dir;
    const bottom = el("div");
    bottom.style.marginTop = "18px";
    const btn = el("button", "btn btn-primary btn-block", t("enterManage"));
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
    const h = el("h3", "", t("cannotDeploy"));
    h.style.color = "var(--danger)";
    const d = el("div", "pdesc");
    d.style.color = "var(--danger)";
    const ul = el("ul", "small");
    ul.style.cssText = "margin:0;padding-left:20px;line-height:2";
    if (permDenied) {
      d.textContent = t("dockerPermDesc");
      const row = el("div");
      row.style.cssText = "display:flex;gap:8px;align-items:center;margin:10px 0;flex-wrap:wrap";
      const sudoPwI = el("input", "pinput");
      sudoPwI.type = "password";
      sudoPwI.placeholder = S.pwdAuth ? t("sudoPwPhSame") : t("sudoPwPhKey");
      sudoPwI.style.width = "240px";
      const sudoBtn = el("button", "btn btn-primary", t("enableSudo"));
      sudoBtn.onclick = async () => {
        sudoBtn.disabled = true;
        sudoBtn.textContent = t("verifying");
        try {
          await api("/api/enable-sudo", { method: "POST", body: { password: sudoPwI.value } });
          route(); // 重新探测
        } catch (e2) {
          block.append(alertBar("err", "✗ " + e2.message));
          sudoBtn.disabled = false;
          sudoBtn.textContent = t("enableSudo");
        }
      };
      row.append(sudoPwI, sudoBtn);
      const manual = el("div", "small muted");
      manual.textContent = t("manualFix", { u: (S.connStr || "").split("@")[0] });
      block.append(h, d, row, manual);
      slot.append(block);
      return;
    }
    d.textContent = t("needDockerDesc");
    const li1 = el("li");
    li1.append(el("b", "", t("nasSyno")), document.createTextNode(t("synoHint")));
    const li2 = el("li");
    li2.append(el("b", "", t("nasQnap")), document.createTextNode(t("qnapHint")));
    const li3 = el("li");
    li3.append(el("b", "", t("nasOther")), document.createTextNode(t("otherHint")));
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
    const h41 = el("h4", "", t("fullDeployTitle"));
    const giteaPort = p.port_gitea_busy ? 3001 : 3000;
    const p1 = el("p", "", t("fullDeployDesc", { p: giteaPort }));
    c1.append(h41, p1);
    c1.onclick = () => { location.hash = "#/deploy?mode=full"; };
    const c2 = el("div", "branch-card");
    const h42 = el("h4", "", t("extTitle"));
    const p2 = el("p", "", t("extDesc"));
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
  const btn = el("button", "btn btn-primary btn-block", t("startDeploy"));
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
    content.append(el("h2", "pagehead", t("extDeployHead")));
    content.append(el("div", "pagesub", t("extDeploySub", { d: p.gitea_detail ? t("detailParens", { d: p.gitea_detail }) : "" })));
  } else {
    content.append(el("h2", "pagehead", t("fullDeployHead")));
    content.append(el("div", "pagesub", t("fullDeploySub")));
  }
  // 未正式开始部署前可回退上一步（开始部署后 runDeployView 清屏锁表单）
  const backProbe = el("a", "small", t("backToProbe"));
  backProbe.href = "javascript:void(0)";
  backProbe.onclick = () => { location.hash = "#/probe"; };
  content.append(backProbe);

  const errSlot = el("div");
  content.append(errSlot);
  const card = el("div", "pcard");
  // 部署目录（原样传给远端，~ 由远端 sh 展开，前端不做本地展开）
  const dirI = pinput("mono", "~/okryptos", "230px");
  const browse = el("button", "btn", t("browse"));
  browse.onclick = () => openDirPicker(dirI);
  card.append(prow(t("deployDir"), [dirI, browse]));
  let giteaPortI = null;
  if (mode === "full") {
    giteaPortI = pinput("mono", giteaPortDef, "90px");
    card.append(prow(t("giteaPort"), [giteaPortI]));
    if (p.port_gitea_busy) {
      const w = el("div", "fb-warn", t("portAvoided", { p: 3000 }));
      w.style.margin = "-4px 0 4px 120px";
      card.append(w);
    }
  }
  const okPortI = pinput("mono", okPortDef, "90px");
  card.append(prow(t("okPort"), [okPortI]));
  if (p.port_ok_busy) {
    const w = el("div", "fb-warn", t("portAvoided", { p: 3100 }));
    w.style.margin = "-4px 0 4px 120px";
    card.append(w);
  }
  const tagI = pinput("mono", "latest", "140px");
  card.append(prow(t("imageTag"), [tagI, el("span", "muted small", "z7dream/okryptos-okserver:latest")]));
  let giteaUrlI = null, tokenI = null, smokeFb = null;
  if (mode === "external") {
    giteaUrlI = pinput("mono", S.connHost ? "http://" + S.connHost + ":3000" : "", "230px");
    if (!S.connHost) giteaUrlI.placeholder = "http://192.168.1.10:3000";
    card.append(prow(t("giteaUrl"), [giteaUrlI]));
    tokenI = pinput("", "", "230px", "password");
    tokenI.placeholder = "gitea admin token";
    const smokeBtn = el("button", "btn", t("testGitea"));
    card.append(prow(t("adminToken"), [tokenI, smokeBtn]));
    smokeFb = el("div");
    smokeFb.style.margin = "-4px 0 4px 120px";
    card.append(smokeFb);
    smokeBtn.onclick = async () => {
      smokeFb.innerHTML = "";
      smokeFb.append(el("span", "small muted", t("testing")));
      try {
        await api("/api/smoke-external", { method: "POST", body: { gitea_url: giteaUrlI.value.trim(), admin_token: tokenI.value } });
        smokeFb.innerHTML = "";
        smokeFb.append(el("span", "fb-ok", t("smokeOk")));
      } catch (e) {
        smokeFb.innerHTML = "";
        smokeFb.append(el("span", "fb-err", "✗ " + e.message));
      }
    };
  }
  const btnWrap = el("div");
  btnWrap.style.marginTop = "18px";
  const deployBtn = el("button", "btn btn-primary btn-block", t("startDeploy"));
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
  const h = el("h3", "", t("checklistTitle"));
  h.style.color = "var(--warn)";
  card.append(h);
  card.append(el("div", "pdesc", t("checklistDesc")));
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
  const btn = el("button", "btn btn-primary", t("startDeploy"));
  btn.disabled = true;
  btn.onclick = onConfirm;
  row.append(btn, el("span", "small muted", t("checklistHint")));
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
  st.append(spin, document.createTextNode(t("deploying")));
  const bar = el("div", "bar");
  const barI = el("i");
  barI.style.width = "0%";
  bar.append(barI);
  stepbar.append(st, bar);
  content.append(stepbar);
  content.append(el("h2", "pagehead", t("deployHead")));
  content.append(el("div", "pagesub", t("deploySub")));
  const summary = el("div", "summary");
  summary.append(document.createTextNode(t("sumDir")));
  summary.append(el("b", "mono", spec.dir));
  if (spec.mode === "full") {
    summary.append(document.createTextNode(t("sumGiteaPort")));
    summary.append(el("b", "mono", String(spec.gitea_port)));
  }
  summary.append(document.createTextNode(t("sumOkPort")));
  summary.append(el("b", "mono", String(spec.ok_port)));
  summary.append(document.createTextNode(t("sumImage")));
  summary.append(el("b", "mono", spec.tag));
  summary.append(el("span", "muted", t("readonly")));
  content.append(summary);
  const logSlot = el("div");
  content.append(logSlot);
  const doneSlot = el("div");
  content.append(doneSlot);
  const logHint = el("div", "small muted", t("logHint"));
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
      st.append(sp, document.createTextNode(t("currentStep") + ev.step));
      barI.style.width = Math.min(90, seenSteps.size * 12) + "%";
    }
    if (done) return;
    if (ev.text.indexOf("任务完成：") >= 0) {
      done = true;
      barI.style.width = "100%";
      st.textContent = t("deployDone");
      showDeployDone(doneSlot, spec);
    } else if (ev.text.indexOf("失败：") >= 0) {
      done = true;
      st.textContent = t("deployFailed");
      doneSlot.append(alertBar("err", "✗ " + ev.text + t("deployFailSuffix")));
      // 失败即任务结束：允许回表单改配置重试（hash 未变，直接重跑 route 渲染表单）
      const backBtn = el("button", "btn", t("backToEdit"));
      backBtn.style.marginTop = "10px";
      backBtn.onclick = () => route();
      doneSlot.append(backBtn);
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
  slot.append(alertBar("ok", t("doneRunning", { h: S.connHost || "NAS" })));
  if (pw) {
    slot.append(pwdCard(t("rootPwTitle"), pw, t("rootPwWarn")));
  } else {
    slot.append(alertBar("err", t("rootPwFail")));
  }
  const next = pcard(t("nextSteps"));
  const ol = el("ol", "steps");
  ol.style.cssText = "margin:8px 0 0;padding-left:20px";
  const li1 = el("li");
  li1.append(document.createTextNode(t("nextLi1a")), el("b", "", t("nextLi1b")), document.createTextNode(t("nextLi1c")));
  li1.append(el("span", "mono", "http://" + (S.connHost || "<nas>") + ":" + spec.ok_port));
  const li2 = el("li");
  li2.append(document.createTextNode(t("nextLi2a")), el("span", "mono", "root"), document.createTextNode(t("nextLi2b")));
  const li3 = el("li", "", t("nextLi3"));
  ol.append(li1, li2, li3);
  next.append(ol);
  slot.append(next);
  const row = el("div", "prow");
  const manage = el("button", "btn btn-primary", t("enterManage"));
  manage.onclick = () => { S.deployDir = spec.dir; location.hash = "#/manage"; };
  const close = el("button", "btn", t("closeWindow"));
  close.onclick = () => window.close();
  row.append(manage, close);
  slot.append(row);
}

/* ================== 管理模式页 ================== */
function pageManage(content) {
  content.append(el("h2", "pagehead", t("manageTitle")));
  const sub = el("div", "pagesub");
  sub.append(document.createTextNode(t("deployDirPrefix")));
  sub.append(el("span", "mono", S.deployDir || t("unknownProbe")));
  sub.append(document.createTextNode(" · "));
  const re = el("a", "", t("reprobe"));
  re.href = "javascript:void(0)";
  re.onclick = () => { location.hash = "#/probe"; };
  const disc = el("a", "", t("disconnect"));
  disc.href = "javascript:void(0)";
  disc.onclick = async () => {
    try { await api("/api/disconnect", { method: "POST" }); } catch (e) { /* 忽略 */ }
    setBadge(false, t("badgeDisconnected"));
    S.connStr = ""; S.connHost = ""; S.probe = null; S.deployDir = "";
    location.hash = "#/connect";
  };
  sub.append(re, document.createTextNode(" · "), disc);
  content.append(sub);

  if (!S.deployDir) {
    const a = alertBar("err", t("noDirWarn"));
    content.append(a);
    const btn = el("button", "btn btn-primary", t("goProbe"));
    btn.onclick = () => { location.hash = "#/probe"; };
    content.append(btn);
    return;
  }

  const errSlot = el("div");
  content.append(errSlot);
  const statSlot = el("div");
  content.append(statSlot);
  statSlot.append(el("div", "muted", t("statusLoading")));
  let status = null;

  // ---- 升级 ----
  const upgrade = pcard(t("upgrade"), t("upgradeDesc"));
  const upRow = el("div", "prow");
  const curTag = el("span", "muted small", t("curPrefix") + "… →");
  const tagI = pinput("mono", "latest", "120px");
  const upBtn = el("button", "btn btn-primary", t("upgrade"));
  // 同 tag 重拉：镜像 tag 被覆盖更新（测试期常见）时强制 pull 刷新
  const reBtn = el("button", "btn", t("repull"));
  const verHint = el("span", "small");
  upRow.append(curTag, tagI, upBtn, reBtn, verHint);
  upgrade.append(upRow);

  // ---- 查看日志 ----
  const viewLogs = pcard(t("viewLogs"), t("viewLogsDesc"));
  const lgRow = el("div", "prow");
  const tailSel = el("select", "pselect mono");
  for (const n of ["100", "200", "500", "2000"]) {
    const op = el("option", "", n);
    op.value = n;
    if (n === "200") op.selected = true;
    tailSel.append(op);
  }
  const pullBtn = el("button", "btn", t("pullLogs"));
  lgRow.append(tailSel, el("span", "muted small", t("linesUnit")), pullBtn, el("span", "small muted", t("pullHint")));
  viewLogs.append(lgRow);

  // ---- 重置 root 密码 ----
  const reset = pcard(t("resetTitle"), t("resetDesc"));
  reset.classList.add("card-danger");
  reset.style.borderWidth = "2px";
  const rsRow = el("div", "prow");
  const rsI = pinput("", "", "180px");
  rsI.placeholder = t("resetPh");
  const rsBtn = el("button", "btn btn-danger", t("resetBtn"));
  rsBtn.disabled = true;
  rsI.oninput = () => { rsBtn.disabled = rsI.value !== "RESET"; };
  rsRow.append(rsI, rsBtn);
  const rsResult = el("div");
  reset.append(rsRow, rsResult);

  // ---- 备份 ----
  const backup = pcard(t("backupTitle"), t("backupDesc"));
  const bkRow = el("div", "prow");
  const bkBtn = el("button", "btn", t("backupNow"));
  bkRow.append(bkBtn);
  backup.append(bkRow);

  // ---- 恢复 ----
  const restore = pcard(t("restoreTitle"));
  restore.classList.add("card-danger");
  restore.style.borderWidth = "2px";
  const rtRow = el("div", "prow");
  const fileI = el("input");
  fileI.type = "file";
  fileI.accept = ".tar";
  fileI.style.display = "none";
  const pickBtn = el("button", "btn", t("pickFile"));
  const fileName = el("span", "mono small muted", t("noneSelected"));
  const rtI = pinput("", "", "180px");
  rtI.placeholder = t("restorePh");
  pickBtn.onclick = () => fileI.click();
  // 按钮需同时满足：已选文件 + 确认词 RESTORE（照卸载卡 DELETE 模式）
  const rtCheck = () => { rtBtn.disabled = !(fileI.files.length && rtI.value === "RESTORE"); };
  fileI.onchange = () => { fileName.textContent = fileI.files.length ? fileI.files[0].name : t("noneSelected"); rtCheck(); };
  rtI.oninput = rtCheck;
  rtRow.append(fileI, pickBtn, fileName, rtI);
  const rtWarn = el("div", "fb-err", t("restoreWarn"));
  rtWarn.style.margin = "2px 0 8px";
  const rtBtn = el("button", "btn btn-danger", t("restoreBtn"));
  rtBtn.disabled = true;
  restore.append(rtRow, rtWarn, rtBtn);

  // ---- 卸载 ----
  const uninstall = pcard(t("uninstallTitle"), t("uninstallDesc"));
  uninstall.classList.add("card-danger");
  uninstall.style.borderWidth = "2px";
  const unRow = el("div", "prow");
  const unI = pinput("", "", "180px");
  unI.placeholder = t("deletePh");
  unRow.append(unI);
  const delLab = el("label", "ck");
  const delCb = el("input");
  delCb.type = "checkbox";
  delLab.append(delCb, document.createTextNode(t("deleteData")));
  const unBtnRow = el("div", "prow");
  unBtnRow.style.marginTop = "12px";
  const unBtn = el("button", "btn btn-danger", t("uninstallTitle"));
  unBtn.disabled = true;
  unI.oninput = () => { unBtn.disabled = unI.value !== "DELETE"; };
  unBtnRow.append(unBtn);
  uninstall.append(unRow, delLab, unBtnRow);

  // ---- 操作日志区（SSE）与容器日志面板 ----
  const opsSlot = el("div");
  const foldSlot = el("div");
  foldSlot.style.marginBottom = "12px";
  renderFold(foldSlot, null, 0, "", pullLogs);
  // 六张操作卡两两一行（升级+日志 / 重置+备份 / 恢复+卸载），
  // SSE 操作日志放在升级/日志卡之下，容器日志折叠面板沉到页底
  const row1 = el("div", "cardrow"); row1.append(upgrade, viewLogs);
  const row2 = el("div", "cardrow"); row2.append(reset, backup);
  const row3 = el("div", "cardrow"); row3.append(restore, uninstall);
  content.append(row1, opsSlot, row2, row3, foldSlot);

  // 版本号比较：vX.Y.Z 逐段数值比（非标准串视为最小）
  function semverGt(a, b) {
    const pa = (a || "").replace(/^v/, "").split(".").map(Number);
    const pb = (b || "").replace(/^v/, "").split(".").map(Number);
    for (let i = 0; i < 3; i++) {
      if ((pa[i] || 0) !== (pb[i] || 0)) return (pa[i] || 0) > (pb[i] || 0);
    }
    return false;
  }
  const curImageTag = () => (status && status.image ? status.image.split(":").pop() : "latest");

  // 状态查询（升级完成后也会重调，刷新版本显示）
  function loadStatus() {
    return api("/api/status?dir=" + encodeURIComponent(S.deployDir)).then((st) => {
      status = st;
      statSlot.innerHTML = "";
      const row = el("div", "stat3");
      for (const c of st.containers || []) {
        const card = el("div", "pcard");
        card.append(el("div", "cap", c.name + t("containerSuffix")));
        const val = el("div", "val");
        const dot = el("span", "dot");
        const up = /^Up/i.test(c.status || "");
        dot.style.background = up ? "var(--ok)" : "var(--danger)";
        val.append(dot, document.createTextNode(up ? t("runningPrefix") + c.status : (c.status || t("statusUnknown"))));
        card.append(val);
        row.append(card);
      }
      const info = el("div", "pcard");
      info.append(el("div", "cap", t("imageDiskCap")));
      const val = el("div", "val mono", (st.image || t("statusUnknown")) + (st.version ? t("runningVer", { v: st.version }) : "") + " · " + (st.disk_usage || "?"));
      val.style.fontSize = "12.5px";
      info.append(val);
      row.append(info);
      statSlot.append(row);
      const tag = curImageTag();
      // 当前显示：优先运行版本（meta 自报），镜像 tag 括注；取不到运行版本就显示 tag
      curTag.textContent = "";
      curTag.append(document.createTextNode(t("curPrefix")));
      const m = el("span", "mono", st.version ? st.version + (st.version !== tag ? t("detailParens", { d: tag }) : "") : tag);
      curTag.append(m, document.createTextNode(" →"));
      // 有新版本：提示并把输入框预填成最新版；否则预填当前 tag
      const cur = st.version || tag;
      if (st.latest_version && semverGt(st.latest_version, cur)) {
        verHint.textContent = "";
        verHint.append(el("span", "", t("newVersion")), el("b", "", st.latest_version));
        verHint.style.color = "var(--warn, #b7791f)";
        tagI.value = st.latest_version;
      } else {
        verHint.textContent = st.latest_version ? t("upToDate") : "";
        verHint.style.color = "var(--ok)";
        tagI.value = tag;
      }
    }).catch((e) => {
      statSlot.innerHTML = "";
      errSlot.append(alertBar("err", t("statusFail") + e.message));
    });
  }
  loadStatus();

  // 操作通用：起任务 → SSE 日志 → 完成/失败提示。onDone(ev) 返回 true 表示已处理完成。
  function watchOps(t0, onDone) {
    let done = false;
    mountLogPane(opsSlot, (ev) => {
      const ts = new Date(ev.ts).getTime();
      if (ts && ts < t0 - 1000) return;
      if (done) return;
      if (ev.text.indexOf("任务完成：") >= 0) { done = true; onDone(null); }
      else if (ev.text.indexOf("失败：") >= 0) { done = true; onDone(new Error(ev.text)); }
    }, "h480");
  }

  function doUpgrade(tag) {
    errSlot.innerHTML = "";
    const t0 = Date.now();
    api("/api/upgrade", { method: "POST", body: { dir: S.deployDir, tag: tag } }).then(() => {
      watchOps(t0, (err) => {
        if (err) errSlot.append(alertBar("err", "✗ " + err.message));
        else { errSlot.append(alertBar("ok", t("upgradeDone"))); loadStatus(); }
      });
    }).catch((e) => { errSlot.append(alertBar("err", "✗ " + e.message)); });
  }

  upBtn.onclick = () => doUpgrade(tagI.value.trim());
  reBtn.onclick = () => doUpgrade(curImageTag());

  async function pullLogs() {
    renderFold(foldSlot, null, 0, t("pulling"), pullLogs);
    try {
      const r = await api("/api/remote-logs?dir=" + encodeURIComponent(S.deployDir) + "&tail=" + tailSel.value);
      renderFold(foldSlot, r.logs || "", tailSel.value, "", pullLogs);
    } catch (e) {
      renderFold(foldSlot, null, 0, "✗ " + e.message, pullLogs);
    }
  }
  pullBtn.onclick = pullLogs;

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
          rsResult.append(pwdCard(t("newRootPw"), r.root_password, t("newRootPwWarn")));
        } catch (e2) {
          rsResult.append(alertBar("err", t("resetReadFail") + e2.message));
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
        else errSlot.append(alertBar("ok", t("restoreDone")));
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
        errSlot.append(alertBar("ok", t("uninstalled")));
        setTimeout(() => { location.hash = "#/connect"; }, 1200);
      });
    } catch (e) { errSlot.append(alertBar("err", "✗ " + e.message)); unBtn.disabled = false; }
  };
}

// 底部容器日志面板（照原型 logfold 折叠块 + .log h480，非 SSE，一次性拉取）。
// onOpen：折叠态点头行也可触发拉取（"点击展开"不再是死文案）。
function renderFold(slot, logs, tail, hint, onOpen) {
  slot.innerHTML = "";
  const open = logs !== null;
  const title = logs !== null ? t("foldTitleTail", { n: tail }) : t("foldTitle");
  const head = el("div", "logfold", (open ? "▾ " : "▸ ") + title + (open ? t("foldClose") : t("foldOpen")));
  head.onclick = () => {
    if (open) renderFold(slot, null, 0, "", onOpen);
    else if (onOpen) onOpen();
  };
  slot.append(head);
  if (open) {
    const body = el("div", "log h480");
    body.style.borderRadius = "0 0 8px 8px";
    body.textContent = logs || t("noLogs");
    slot.append(body);
  } else if (hint) {
    const body = el("div", "log h480");
    body.style.borderRadius = "0 0 8px 8px";
    body.textContent = hint;
    slot.append(body);
  }
}

// 顶栏中/EN 切换胶囊（index.html 静态放置，这里只接管交互与初始高亮）
document.querySelectorAll("#lang-seg button").forEach((b) => {
  b.onclick = () => setLang(b.getAttribute("data-lang"));
});
setLang(LANG, true); // 应用持久化语言（title/html lang/徽标），不触发额外 route
route();
