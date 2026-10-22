(async () => {
  var ascii = function (o) { return JSON.stringify(o).replace(/[\u0080-\uffff]/g, function (c) { return "\\u" + c.charCodeAt(0).toString(16).padStart(4, "0"); }); };
  var sleep = function (ms) { return new Promise(function (r) { setTimeout(r, ms); }); };
  var TOKEN = window.__WB_TOKEN__ || "";
  var btn = document.getElementById("startBtn");
  var sp = btn.querySelector(".btn-spinner");
  var ar = btn.querySelector(".btn-arc");
  var api = function (path, method, body) {
    return fetch(path, { method: method || "GET", headers: { "X-WB-Token": TOKEN, "Content-Type": "application/json" }, body: body ? JSON.stringify(body) : undefined })
      .then(function (r) { return r.text(); }).then(function (t) { try { return JSON.parse(t); } catch (e) { return { raw: t }; } });
  };
  var read = function (tag) { return { tag: tag, state: btn.getAttribute("data-state"),
    spinner: getComputedStyle(sp).opacity, arc: getComputedStyle(ar).opacity }; };
  var out = { loadSettled: [], stoppedTimeline: [] };

  // 1) 等到页面加载后的自动触发完成并稳定（自动触发只在 900ms 发生一次）
  for (var i = 0; i < 12; i++) { await sleep(500); }
  out.loadSettled.push(read("after-6s"));

  // 2) 停引擎 → 逐 100ms 采样加载环过渡（证明 stopped 态最终不显示旋转环）
  await api("/api/engine/stop", "POST", {});
  for (var k = 0; k < 14; k++) { await sleep(100); out.stoppedTimeline.push(read((k + 1) * 100 + "ms")); }

  // 3) 再点一次（手动兜底）→ busy 态必须有快环
  var origFetch = window.fetch;
  var calls = 0;
  window.fetch = function () { if (String(arguments[0]).indexOf("/api/engine/start") >= 0) calls++; return origFetch.apply(this, arguments); };
  btn.click();
  var busyTimeline = [];
  for (var m = 0; m < 8; m++) { await sleep(50); busyTimeline.push(read("busy+" + ((m + 1) * 50) + "ms")); }
  window.fetch = origFetch;
  out.busyTimeline = busyTimeline;
  out.startCallsFromSingleManualClick = calls;
  out.final = read("final");
  return ascii(out);
})()