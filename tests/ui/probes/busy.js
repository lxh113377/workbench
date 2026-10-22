(async () => {
  var ascii = function (o) { return JSON.stringify(o).replace(/[\u0080-\uffff]/g, function (c) { return "\\u" + c.charCodeAt(0).toString(16).padStart(4, "0"); }); };
  var sleep = function (ms) { return new Promise(function (r) { setTimeout(r, ms); }); };
  var TOKEN = window.__WB_TOKEN__ || "";
  var api = function (path, method, body) {
    return fetch(path, { method: method || "GET", headers: { "X-WB-Token": TOKEN, "Content-Type": "application/json" }, body: body ? JSON.stringify(body) : undefined })
      .then(function (r) { return r.text(); }).then(function (t) { try { return JSON.parse(t); } catch (e) { return { raw: t }; } });
  };
  var btn = document.getElementById("startBtn");
  var sp = btn.querySelector(".btn-spinner");
  var ar = btn.querySelector(".btn-arc");
  var snap = function (tag) { return { tag: tag, state: btn.getAttribute("data-state"),
    label: (btn.querySelector(".btn-label") || {}).textContent, disabled: btn.disabled,
    ariaBusy: btn.getAttribute("aria-busy"), spinner: getComputedStyle(sp).opacity,
    arc: getComputedStyle(ar).opacity, live: (document.getElementById("homeStatus") || {}).textContent }; };

  // 1) 把引擎停到 stopped，拿到可点的按钮
  await api("/api/engine/stop", "POST", {});
  for (var i = 0; i < 40; i++) { await sleep(250); if (btn.getAttribute("data-state") === "stopped" || btn.getAttribute("data-state") === "idle") break; }
  await sleep(400);
  var before = snap("before-click");

  // 2) 确定性卡在 busy：把 /api/engine/start 的响应延迟 6s。
  //    这样「加载中」这个瞬时态变成可稳定观测/可截图的窗口，而不是靠运气采样。
  var origFetch = window.fetch;
  window.fetch = function () {
    var args = arguments;
    if (String(args[0]).indexOf("/api/engine/start") >= 0) {
      return new Promise(function (resolve) {
        setTimeout(function () { resolve(origFetch.apply(window, args)); }, 6000);
      });
    }
    return origFetch.apply(window, args);
  };
  btn.click();
  var during = [];
  for (var k = 0; k < 10; k++) { await sleep(100); during.push(snap("busy+" + ((k + 1) * 100) + "ms")); }
  return ascii({ beforeClick: before, busyWindow: during,
    stableBusyState: during[during.length - 1] });
})()