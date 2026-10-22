(async () => {
  var btn = document.getElementById("startBtn");
  var label = function () { return (btn.querySelector(".btn-label") || {}).textContent; };
  var sleep = function (ms) { return new Promise(function (r) { setTimeout(r, ms); }); };
  var out = { url: location.href, states: [] };
  out.states.push({ t: "t0", state: btn.getAttribute("data-state"), label: label() });
  for (var i = 1; i <= 12; i++) { await sleep(400); out.states.push({ t: (i*400) + "ms", state: btn.getAttribute("data-state"), label: label() }); }
  out.final = { state: btn.getAttribute("data-state"), label: label() };
  return JSON.stringify(out);
})()