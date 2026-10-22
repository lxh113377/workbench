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
  var g0 = (document.getElementById("govStat") || {}).textContent;
  var rf = document.getElementById("govRefresh");
  if (rf) { rf.click(); await sleep(1500); out.push({ step: "refresh", toast: toast(), statChanged: g0 !== (document.getElementById("govStat") || {}).textContent }); }
  var sm = document.getElementById("govSample");
  if (sm) { sm.click(); await sleep(2000); out.push({ step: "sample", toast: toast(), stat: (document.getElementById("govStat") || {}).textContent }); }
  out.push({ step: "cards-rendered", cards: document.querySelectorAll(".gov-card,.gov-banner").length });
  return ascii({ page: "governance", steps: out, errors: errs.slice(0, 8) });
})()