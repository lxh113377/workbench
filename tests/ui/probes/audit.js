(() => {
  // audit.js —— 设计质量的量化读数（只取数、不判定）。判定在设计审计报告里逐条给出阈值。
  var ascii = function (o) { return JSON.stringify(o).replace(/[\u0080-\uffff]/g, function (c) { return "\\u" + c.charCodeAt(0).toString(16).padStart(4, "0"); }); };
  var rgb = function (s) {
    var m = String(s).match(/rgba?\(([^)]+)\)/);
    if (!m) return null;
    var p = m[1].split(",").map(function (x) { return parseFloat(x.trim()); });
    return { r: p[0], g: p[1], b: p[2], a: p.length > 3 ? p[3] : 1 };
  };
  var lum = function (c) {
    var f = function (v) { v = v / 255; return v <= 0.03928 ? v / 12.92 : Math.pow((v + 0.055) / 1.055, 2.4); };
    return 0.2126 * f(c.r) + 0.7152 * f(c.g) + 0.0722 * f(c.b);
  };
  var blend = function (fg, bg) {
    var a = fg.a;
    return { r: fg.r * a + bg.r * (1 - a), g: fg.g * a + bg.g * (1 - a), b: fg.b * a + bg.b * (1 - a), a: 1 };
  };
  // 背景合成：沿祖先链收集所有背景层，再**自底向上**逐层 over 合成。
  // 原实现是自上向下走的，遇到 alpha<1 的层就把父层当作不透明 —— 会把
  // rgba(255,255,255,.03) 的卡片底算成接近纯白，导致深色面板上的文字被误判成 1.6:1。
  var over = function (fg, bg) {
    var a = fg.a === undefined ? 1 : fg.a;
    return { r: fg.r * a + bg.r * (1 - a), g: fg.g * a + bg.g * (1 - a), b: fg.b * a + bg.b * (1 - a), a: 1 };
  };
  // 渐变背景取所有色标的加权平均（近似视觉平均色），alpha 取最大值。
  var gradAvg = function (img) {
    if (!img || img === "none" || img.indexOf("gradient") < 0) return null;
    var stops = [], re = /rgba?\([^)]+\)/g, m;
    while ((m = re.exec(img))) { var c = rgb(m[0]); if (c) stops.push(c); }
    if (!stops.length) return null;
    var sr = 0, sg = 0, sb = 0, sa = 0, maxA = 0;
    for (var i = 0; i < stops.length; i++) { var s = stops[i]; sr += s.r * s.a; sg += s.g * s.a; sb += s.b * s.a; sa += s.a; if (s.a > maxA) maxA = s.a; }
    if (sa <= 0) return null;
    return { r: sr / sa, g: sg / sa, b: sb / sa, a: maxA };
  };
  var bgOf = function (el) {
    var layers = [], cur = el;
    while (cur && cur !== document.documentElement) {
      var st = getComputedStyle(cur);
      layers.push({ bg: rgb(st.backgroundColor), grad: gradAvg(st.backgroundImage) });
      if (cur === document.body) break;
      cur = cur.parentElement;
    }
    var base = { r: 7, g: 8, b: 10, a: 1 }; // --bg #07080a
    for (var i = layers.length - 1; i >= 0; i--) {
      var L = layers[i];
      if (L.grad) base = over(L.grad, base);
      if (L.bg && L.bg.a > 0) base = over(L.bg, base);
    }
    return base;
  };
  var ratio = function (a, b) { var l1 = lum(a), l2 = lum(b); var hi = Math.max(l1, l2), lo = Math.min(l1, l2); return Math.round(((hi + 0.05) / (lo + 0.05)) * 100) / 100; };
  var visible = function (el) {
    var b = el.getBoundingClientRect();
    var st = getComputedStyle(el);
    if (!(b.width > 0 && b.height > 0 && st.visibility !== "hidden" && st.display !== "none" && parseFloat(st.opacity) > 0.02)) return false;
    // 故意不可见的元素不计入对比度审计（否则会把「隐藏后门」判成缺陷）：
    //   · .sr-only / aria-hidden="true"：读屏可达、视觉不占位
    //   · color:transparent：设计上就该隐形（靠 hover/focus 显形）
    if (el.closest('.sr-only, [aria-hidden="true"]')) return false;
    var c = rgb(st.color);
    if (c && c.a === 0) return false;
    if (String(st.color).indexOf("rgba(0, 0, 0, 0)") >= 0) return false;
    return true;
  };
  // ── 1) 文本对比度：每个含「非空白自有文本」的元素算一次 ──
  var lowContrast = [];
  var textStats = { nodes: 0, minRatio: 99, minSelector: "", below45: 0, below3: 0 };
  var all = Array.prototype.slice.call(document.querySelectorAll("body *"));
  for (var i = 0; i < all.length; i++) {
    var el = all[i];
    if (el.tagName === "SCRIPT" || el.tagName === "STYLE" || el.tagName === "NOSCRIPT") continue;
    var own = "";
    for (var k = 0; k < el.childNodes.length; k++) { var n = el.childNodes[k]; if (n.nodeType === 3) own += n.nodeValue; }
    own = own.replace(/\s+/g, " ").trim();
    if (!own) continue;
    if (!visible(el)) continue;
    var st = getComputedStyle(el);
    var fs = parseFloat(st.fontSize) || 0;
    var fg = rgb(st.color); if (!fg) continue;
    var bg = bgOf(el);
    var fgOnBg = fg.a < 1 ? blend(fg, bg) : fg;
    var r = ratio(fgOnBg, bg);
    textStats.nodes++;
    if (r < textStats.minRatio) { textStats.minRatio = r; textStats.minSelector = el.tagName.toLowerCase() + (el.className ? "." + String(el.className).split(" ")[0] : "") + " «" + own.slice(0, 18) + "»"; }
    var large = fs >= 18.66 || (fs >= 14 && (parseInt(st.fontWeight, 10) >= 700));
    var want = large ? 3.0 : 4.5;
    if (r < 4.5) textStats.below45++;
    if (r < 3) textStats.below3++;
    if (r < want) lowContrast.push({ sel: el.tagName.toLowerCase() + (el.id ? "#" + el.id : "") + (el.className ? "." + String(el.className).split(" ").join(".") : ""), text: own.slice(0, 24), ratio: r, want: want, fontSize: fs });
  }
  // ── 2) 可点区域：交互元素的实际点击尺寸 ──
  var small = [];
  var inter = document.querySelectorAll("button, a[href], input, select, textarea, [onclick], [data-act]");
  var minH = 9999, minSel = "";
  for (var j = 0; j < inter.length; j++) {
    var e = inter[j];
    if (!visible(e)) continue;
    if (e.type === "hidden") continue;
    var b = e.getBoundingClientRect();
    var h = Math.round(b.height), w = Math.round(b.width);
    if (h < minH) { minH = h; minSel = e.tagName.toLowerCase() + (e.id ? "#" + e.id : "") + (e.className ? "." + String(e.className).split(" ")[0] : "") + " «" + (e.textContent || "").trim().slice(0, 12) + "»"; }
    if (h < 44) small.push({ sel: minSel, h: h, w: w, small: h < 32 });
  }
  small = small.slice(0, 40);
  // ── 3) 字号谱：统计各字号出现次数（找 mini 标签与过大的断层） ──
  var sizes = {};
  for (var s = 0; s < all.length; s++) { var el2 = all[s]; if (!visible(el2)) continue; var t = (el2.textContent || "").trim(); if (!t) continue; var f2 = Math.round((parseFloat(getComputedStyle(el2).fontSize) || 0) * 10) / 10; if (f2 > 0) sizes[f2] = (sizes[f2] || 0) + 1; }
  // ── 4) 版心与行长：正文文本块的宽度（以 ch 计） ──
  var longText = [];
  var paras = document.querySelectorAll("p, li, .muted, .note, td, .gov-dim, .doc p");
  for (var q = 0; q < paras.length; q++) {
    var pe = paras[q]; if (!visible(pe)) continue;
    var tx = (pe.textContent || "").trim(); if (tx.length < 40) continue;
    var st2 = getComputedStyle(pe);
    var fs2 = parseFloat(st2.fontSize) || 14;
    var wch = Math.round(pe.getBoundingClientRect().width / (fs2 * 0.5));
    if (wch > 90) longText.push({ sel: pe.tagName.toLowerCase() + (pe.className ? "." + String(pe.className).split(" ")[0] : ""), ch: wch, len: tx.length });
  }
  // ── 5) 焦点可见性：把焦点依次给前 12 个可交互元素，读 outline ──
  var focusIssues = 0, focusTested = 0;
  for (var f = 0; f < Math.min(12, inter.length); f++) {
    var fe = inter[f];
    if (!visible(fe)) continue;
    try { fe.focus(); } catch (e) { continue; }
    focusTested++;
    var cs = getComputedStyle(fe);
    var hasRing = (parseFloat(cs.outlineWidth) || 0) > 0 && cs.outlineStyle !== "none";
    var hasShadow = cs.boxShadow && cs.boxShadow !== "none" && cs.boxShadow.indexOf("rgba(0, 0, 0, 0)") < 0;
    if (!hasRing && !hasShadow) focusIssues++;
  }
  try { document.body.focus(); } catch (e) {}
  // ── 6) 动效：prefers-reduced-motion 是否被真正尊重（读数给外部判） ──
  var animated = 0;
  for (var a2 = 0; a2 < all.length; a2++) { var an = getComputedStyle(all[a2]).animationName; if (an && an !== "none") animated++; }
  // ── 7) 页面级读数 ──
  var sc = document.scrollingElement;
  var over = [];
  for (var o = 0; o < all.length; o++) { var bb = all[o].getBoundingClientRect(); if (bb.width > 0 && bb.height > 0 && (bb.right > innerWidth + 2 || bb.left < -2)) over.push(all[o].tagName + "." + String(all[o].className || "").slice(0, 20)); }
  // ── 8) 布局稳定性（CLS）：累计 layout-shift（无输入触发的非预期位移） ──
  // 读 window.__cls 需在页面加载前埋点；没埋到就退回「当前 DOM 与首次绘制后位置的偏移」近似。
  var cls = (typeof window.__cls === "number") ? window.__cls : null;
  // ── 9) 首屏信息密度：一屏内可见的卡片/读数块数量（衡量「留白 vs 密度」是否失衡） ──
  var firstScreen = 0;
  for (var fs1 = 0; fs1 < all.length; fs1++) {
    var eb = all[fs1].getBoundingClientRect();
    if (eb.width <= 0 || eb.height <= 0) continue;
    if (eb.top < innerHeight && eb.bottom > 0) {
      var cl = String(all[fs1].className || "");
      if (/(^| )(card|kpi|gov-kv|gov-cell|gov-check|kv-item|item)( |$)/.test(cl)) firstScreen++;
    }
  }
  return ascii({
    cls: cls,
    firstScreenBlocks: firstScreen,
    url: location.pathname + location.hash,
    vp: [innerWidth, innerHeight],
    prefersReducedMotion: matchMedia("(prefers-reduced-motion: reduce)").matches,
    // 取一个必然有动画的元素（首页按钮 / 页签），读实际 animationDuration：
    // 在 reduce 模拟下应为 ~0ms，否则「已尊重系统减少动效」这个声明就是假的。
    animSample: (function () {
      var cand = document.getElementById("startBtn") || document.querySelector(".tab") || document.querySelector(".card");
      if (!cand) return null;
      var s = getComputedStyle(cand);
      return { sel: cand.id ? "#" + cand.id : "." + String(cand.className).split(" ")[0], name: s.animationName, duration: s.animationDuration, transition: s.transitionDuration };
    })(),
    textStats: textStats,
    lowContrast: lowContrast.slice(0, 30),
    smallTargets: small,
    minTarget: { h: minH, sel: minSel },
    fontSizes: sizes,
    longTextLines: longText.slice(0, 12),
    focus: { tested: focusTested, issues: focusIssues },
    animatedElements: animated,
    overflowX: Math.max(0, sc.scrollWidth - sc.clientWidth),
    overflowEls: over.slice(0, 8)
  });
})()