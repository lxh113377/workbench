(async () => {
  var ascii = function (o) { return JSON.stringify(o).replace(/[\u0080-\uffff]/g, function (c) { return "\\u" + c.charCodeAt(0).toString(16).padStart(4, "0"); }); };
  var sleep = function (ms) { return new Promise(function (r) { setTimeout(r, ms); }); };
  var TOKEN = window.__WB_TOKEN__ || "";
  var btn = document.getElementById("startBtn");
  var snap = function (tag) { return { tag: tag, state: btn.getAttribute("data-state"),
    label: (btn.querySelector(".btn-label") || {}).textContent,
    disabled: btn.disabled, busy: btn.getAttribute("aria-busy"),
    spinner: parseFloat(getComputedStyle(btn.querySelector(".btn-spinner")).opacity),
    arc: parseFloat(getComputedStyle(btn.querySelector(".btn-arc")).opacity),
    live: (document.getElementById("homeStatus") || {}).textContent }; };
  var api = function (path, method, body) {
    return fetch(path, { method: method || "GET", headers: { "X-WB-Token": TOKEN, "Content-Type": "application/json" }, body: body ? JSON.stringify(body) : undefined })
      .then(function (r) { return r.text(); }).then(function (t) { try { return JSON.parse(t); } catch (e) { return { raw: t }; } });
  };

  // ── A. 自动加载 → 自动响应 → 自动触发（采样到状态稳定）──
  var timeline = [snap("t0")];
  for (var i = 1; i <= 30; i++) { await sleep(100); timeline.push(snap((i * 100) + "ms")); }
  var compact = timeline.filter(function (v, idx, arr) { return idx === 0 || v.state !== arr[idx - 1].state; });

  // ── B. 防重入：先把引擎停掉（按钮恢复可点），再连点 5 次 ──
  await api("/api/engine/stop", "POST", {});
  var stoppedSeen = null;
  for (var w = 0; w < 40; w++) {
    await sleep(250);
    if (btn.getAttribute("data-state") === "stopped" || btn.getAttribute("data-state") === "idle") { stoppedSeen = snap("stopped"); break; }
  }
  await sleep(400); // 让 opacity 过渡收敛，读数才是静态值而非过渡中值
  var enabledState = snap("before-burst");
  var startCalls = 0;
  var origFetch = window.fetch;
  window.fetch = function () { if (String(arguments[0]).indexOf("/api/engine/start") >= 0) startCalls++; return origFetch.apply(this, arguments); };
  for (var j = 0; j < 5; j++) { btn.click(); await sleep(40); }
  await sleep(1200);
  window.fetch = origFetch;
  var afterBurst = snap("after-burst");
  var restarted = afterBurst.state === "running";

  return ascii({
    transitions: compact,
    statesSeen: compact.map(function (t) { return t.state; }),
    spinnerOnLoadSample: timeline[0].spinner,
    spinnerInStoppedState: enabledState.spinner,
    arcInStoppedState: enabledState.arc,
    stoppedSnapshot: stoppedSeen,
    busySnapshot: compact.filter(function (t) { return t.state === "busy"; })[0] || null,
    burstButtonWasEnabled: !enabledState.disabled,
    burstStartCalls: startCalls,
    burstRestartedEngine: restarted,
    final: snap("final")
  });
})()