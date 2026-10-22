(() => {
  var ascii = function (o) { return JSON.stringify(o).replace(/[\u0080-\uffff]/g, function (c) { return "\\u" + c.charCodeAt(0).toString(16).padStart(4, "0"); }); };
  var rect = function (el) { var b = el.getBoundingClientRect(); return { x: Math.round(b.x), y: Math.round(b.y), w: Math.round(b.width), h: Math.round(b.height), right: Math.round(b.right), bottom: Math.round(b.bottom) }; };
  var sc = document.scrollingElement;
  var btn = document.getElementById("startBtn");
  var over = [];
  document.querySelectorAll("body *").forEach(function (el) {
    var b = el.getBoundingClientRect();
    if (b.width > 0 && b.height > 0 && (b.right > innerWidth + 2 || b.left < -2)) over.push(el.tagName + "." + String(el.className || "").slice(0, 24));
  });
  var visible = [];
  document.querySelectorAll("body *").forEach(function (el) {
    var b = el.getBoundingClientRect();
    var st = getComputedStyle(el);
    if (b.width > 0 && b.height > 0 && st.visibility !== "hidden" && st.display !== "none" && parseFloat(st.opacity) > 0.02) {
      visible.push(el.tagName + (el.id ? "#" + el.id : "") + (el.className ? "." + String(el.className).replace(/ /g, ".") : ""));
    }
  });
  // 可见文案长度：排除 .sr-only / aria-hidden（状态播报）与零尺寸元素，
  // 否则 sr-only 文本会把「首页极简」判据污染成恒 >3（2026-10-19 基线实测 textLen=11）。
  var textVisibleLen = 0;
  try {
    var walker = document.createTreeWalker(document.body, NodeFilter.SHOW_TEXT, null);
    var n;
    while ((n = walker.nextNode())) {
      var t = String(n.nodeValue || "").replace(/\s+/g, " ").trim();
      if (!t) continue;
      var p = n.parentElement;
      if (!p) continue;
      if (p.closest('.sr-only, [aria-hidden="true"]')) continue;
      var pb = p.getBoundingClientRect();
      if (pb.width <= 0 || pb.height <= 0) continue;
      textVisibleLen += t.length;
    }
  } catch (e) { textVisibleLen = -1; }
  var res = performance.getEntriesByType("resource").map(function (e) { return { n: e.name.split("/").pop(), bytes: e.transferSize || e.encodedBodySize || 0, ms: Math.round(e.duration) }; });
  var nav = performance.getEntriesByType("navigation")[0] || {};
  var clipped = 0;
  document.querySelectorAll("body *").forEach(function (el) { var b = el.getBoundingClientRect(); if (b.width > 0 && b.height > 0 && (b.top < -2 || b.bottom > innerHeight + 2)) clipped++; });
  // clipped 是「视口下方的元素数」＝页面长度决定，长页面恒 >0，不能当裁切判据（2026-10-19 基线实测
  // governance-390 clipped=145 但 overflowX=0、overflowEls=[]，即内容全可滚动到达，无裁切）。
  // clippedH 才是真裁切：元素越出视口左右边界（含父级 overflow:hidden 的陷阱，此时 overflowX 仍为 0）。
  var clippedH = 0;
  var clippedHNow = [];
  document.querySelectorAll("body *").forEach(function (el) {
    var b = el.getBoundingClientRect();
    if (b.width > 0 && b.height > 0 && (b.left < -2 || b.right > innerWidth + 2)) { clippedH++; if (clippedHNow.length < 8) clippedHNow.push(el.tagName + "." + String(el.className || "").slice(0, 24)); }
  });
  return ascii({
    url: location.pathname,
    vp: { w: innerWidth, h: innerHeight, dpr: devicePixelRatio },
    buttons: document.querySelectorAll("button").length,
    anchors: document.querySelectorAll("a").length,
    divs: document.querySelectorAll("div").length,
    paragraphs: document.querySelectorAll("p").length,
    images: document.querySelectorAll("img,svg,video,canvas").length,
    inputs: document.querySelectorAll("input,select,textarea").length,
    tables: document.querySelectorAll("table").length,
    visible: visible,
    btn: btn ? {
      state: btn.getAttribute("data-state"),
      busy: btn.getAttribute("aria-busy"),
      disabled: btn.disabled,
      label: (btn.querySelector(".btn-label") || {}).textContent || null,
      title: btn.title,
      rect: rect(btn),
      fontSize: getComputedStyle(btn).fontSize,
      spinner: parseFloat(getComputedStyle(btn.querySelector(".btn-spinner")).opacity),
      arc: parseFloat(getComputedStyle(btn.querySelector(".btn-arc")).opacity),
      insideViewport: rect(btn).x >= 0 && rect(btn).y >= 0 && rect(btn).right <= innerWidth && rect(btn).bottom <= innerHeight,
      minTouchOk: rect(btn).h >= 44
    } : null,
    overflowX: Math.max(0, sc.scrollWidth - sc.clientWidth),
    pageH: sc.scrollHeight,
    clipped: clipped,
    clippedH: clippedH,
    clippedHEls: clippedHNow,
    overflowEls: over.slice(0, 8),
    resources: res,
    navMs: Math.round((nav.duration || 0) * 100) / 100,
    domMs: Math.round(nav.domInteractive || 0),
    textLen: (document.body.innerText || "").trim().length,
    textVisibleLen: textVisibleLen
  });
})()