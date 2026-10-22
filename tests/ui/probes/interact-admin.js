(async () => {
  var ascii = function (o) { return JSON.stringify(o).replace(/[\u0080-\uffff]/g, function (c) { return "\\u" + c.charCodeAt(0).toString(16).padStart(4, "0"); }); };
  var sleep = function (ms) { return new Promise(function (r) { setTimeout(r, ms); }); };
  var TOKEN = window.__WB_TOKEN__ || "";
  var api = function (path, method, body) {
    return fetch(path, { method: method || "GET", headers: { "X-WB-Token": TOKEN, "Content-Type": "application/json" }, body: body ? JSON.stringify(body) : undefined })
      .then(function (r) { return r.text(); }).then(function (t) { try { return JSON.parse(t); } catch (e) { return { raw: t }; } });
  };
  var errs = [];
  window.addEventListener("error", function (e) { errs.push("error: " + e.message); });
  window.addEventListener("unhandledrejection", function (e) { errs.push("reject: " + ((e.reason && e.reason.message) || e.reason)); });
  var toast = function () { var t = document.getElementById("toast"); return t ? (t.textContent || "") : ""; };
  var out = [];
  await api("/api/engine/start", "POST", {});
  await sleep(1200);
  var p = document.getElementById("btnPause"), r = document.getElementById("btnResume");
  var st0 = await api("/api/status");
  out.push({ step: "ensure-running", phase: st0.phase });
  if (p) { p.click(); await sleep(1300); out.push({ step: "click-pause", toast: toast(), phase: (await api("/api/status")).phase }); }
  if (r) { r.click(); await sleep(1300); out.push({ step: "click-resume", toast: toast(), phase: (await api("/api/status")).phase }); }
  // 设置页签：改并发并保存
  var tab = document.querySelector('.tab[data-tab="settings"]');
  if (tab) {
    tab.click(); await sleep(900);
    var inputs = document.querySelectorAll('#tab-settings input');
    if (inputs.length) { inputs[0].value = "3"; }
    var save = document.querySelector('#tab-settings button');
    if (save) { save.click(); await sleep(1200); }
    out.push({ step: "settings-save", toast: toast(), inputs: inputs.length,
      savedConcurrency: (await api("/api/settings")).config ? "read" : "n/a" });
  }
  // 停止 → 页面按钮必须能重新启动
  await api("/api/engine/stop", "POST", {});
  await sleep(2500);
  out.push({ step: "after-stop", phase: (await api("/api/status")).phase, buttonState: "n/a" });
  return ascii({ page: "admin", steps: out, errors: errs.slice(0, 8) });
})()