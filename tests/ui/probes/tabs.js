(async () => {
  var ascii = function (o) { return JSON.stringify(o).replace(/[\u0080-\uffff]/g, function (c) { return "\\u" + c.charCodeAt(0).toString(16).padStart(4, "0"); }); };
  var sleep = function (ms) { return new Promise(function (r) { setTimeout(r, ms); }); };
  var errs = [];
  window.addEventListener("error", function (e) { errs.push(String(e.message)); });
  window.addEventListener("unhandledrejection", function (e) { errs.push(String((e.reason && e.reason.message) || e.reason)); });
  var out = [];
  var tabs = Array.prototype.slice.call(document.querySelectorAll(".tab"));
  for (var i = 0; i < tabs.length; i++) {
    var t = tabs[i];
    t.click();
    await sleep(700);
    var name = t.getAttribute("data-tab");
    var pane = document.getElementById("tab-" + name);
    var txt = pane ? (pane.innerText || "").trim() : "";
    out.push({ tab: name, activeNow: t.classList.contains("active"), paneActive: pane ? pane.classList.contains("active") : false,
      activeCountWhenMeasured: document.querySelectorAll(".tab.active").length,
      visiblePanes: document.querySelectorAll(".tabpane.active").length,
      textLen: txt.length, rows: pane ? pane.querySelectorAll("tr").length : 0,
      inputs: pane ? pane.querySelectorAll("input,select,textarea").length : 0,
      buttons: pane ? pane.querySelectorAll("button").length : 0,
      anomalies: ["undefined", "NaN", "[object Object]"].filter(function (k) { return txt.indexOf(k) >= 0; }) });
  }
  var sc = document.scrollingElement;
  return ascii({ vp: [innerWidth, innerHeight], tabs: out,
    finalActiveTabs: document.querySelectorAll(".tab.active").length,
    finalActivePanes: document.querySelectorAll(".tabpane.active").length,
    errors: errs.slice(0, 8), overflowX: Math.max(0, sc.scrollWidth - sc.clientWidth) });
})()