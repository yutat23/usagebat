// Progressive enhancement. Every control on the page is a form that works on
// its own; this only makes the round trip quieter, so a browser that refuses
// to run the script still gets a working settings screen.
//
// The gain is not really the spinner. A full page reload re-reads the history
// file and redraws every chart in order to change one checkbox, and it throws
// away where the reader had scrolled to.
(function () {
  "use strict";

  document.documentElement.classList.add("js");
  var busy = false;

  function markBusy(on) {
    busy = on;
    document.body.classList.toggle("busy", on);
  }

  function showStatus(message, error) {
    var status = document.getElementById("settings-status");
    if (!status) return;
    status.textContent = message || "";
    status.classList.toggle("error", !!error);
  }

  function label(name) {
    var tabs = document.querySelector(".tabbar");
    return tabs ? tabs.getAttribute("data-" + name + "-label") || "" : "";
  }

  function selectTab(id, updateHash) {
    var panels = document.querySelectorAll("[data-tab-panel]");
    var buttons = document.querySelectorAll("[data-tab-target]");
    var found = false;
    for (var i = 0; i < panels.length; i++) {
      if (panels[i].getAttribute("data-tab-panel") === id) found = true;
    }
    if (!found) id = "overview";
    for (var p = 0; p < panels.length; p++) {
      var panelActive = panels[p].getAttribute("data-tab-panel") === id;
      panels[p].classList.toggle("active", panelActive);
      panels[p].hidden = !panelActive;
    }
    for (var b = 0; b < buttons.length; b++) {
      var buttonActive = buttons[b].getAttribute("data-tab-target") === id;
      buttons[b].setAttribute("aria-selected", buttonActive ? "true" : "false");
      buttons[b].tabIndex = buttonActive ? 0 : -1;
    }
    if (updateHash && window.history && window.history.replaceState) {
      window.history.replaceState(null, "", "#" + id);
    }
  }

  function bindTabs() {
    var buttons = document.querySelectorAll("[data-tab-target]");
    var initial = window.location.hash ? window.location.hash.slice(1) : "overview";
    selectTab(initial, false);
    for (var i = 0; i < buttons.length; i++) {
      buttons[i].addEventListener("click", function (event) {
        selectTab(event.currentTarget.getAttribute("data-tab-target"), true);
      });
      buttons[i].addEventListener("keydown", function (event) {
        if (event.key !== "ArrowLeft" && event.key !== "ArrowRight") return;
        event.preventDefault();
        var all = Array.prototype.slice.call(document.querySelectorAll("[data-tab-target]"));
        var at = all.indexOf(event.currentTarget);
        var direction = event.key === "ArrowRight" ? 1 : -1;
        var next = all[(at + direction + all.length) % all.length];
        next.focus();
        selectTab(next.getAttribute("data-tab-target"), true);
      });
    }
  }

  // Swap in a freshly rendered copy, keeping the scroll position: a settings
  // screen that jumps to the top on every click is worse than one that waits.
  function reload() {
    return fetch(window.location.pathname, {
      credentials: "same-origin",
      headers: { "X-Requested-With": "fetch" },
    })
      .then(function (response) {
        if (!response.ok) throw new Error(response.status);
        return response.text();
      })
      .then(function (html) {
        var next = new DOMParser().parseFromString(html, "text/html");
        var fresh = next.querySelector("main");
        var here = document.querySelector("main");
        if (fresh && here) {
          here.replaceWith(fresh);
        }
        bind();
      });
  }

  function submit(form) {
    if (busy) {
      return;
    }
    showStatus("", false);
    markBusy(true);
    fetch(form.action, {
      method: "POST",
      credentials: "same-origin",
      headers: { "X-Requested-With": "fetch" },
      body: new URLSearchParams(new FormData(form)),
    })
      .then(function (response) {
        if (response.ok) return reload();
        return response.text().then(function (message) {
          throw new Error(message.trim() || response.status);
        });
      })
      .then(function () {
        markBusy(false);
        showStatus(label("saved"), false);
      })
      .catch(function (error) {
        markBusy(false);
        showStatus(label("save-error") + ": " + error.message, true);
      });
  }

  function bind() {
    var forms = document.querySelectorAll("form[data-async]");
    for (var i = 0; i < forms.length; i++) {
      forms[i].addEventListener("submit", function (event) {
        event.preventDefault();
        submit(event.currentTarget);
      });
      var fields = forms[i].querySelectorAll('input[name="value"], select[name="value"]');
      for (var j = 0; j < fields.length; j++) {
        fields[j].addEventListener("change", function (event) {
          var form = event.currentTarget.form;
          if (form && form.reportValidity()) submit(form);
        });
      }
    }
    bindTabs();
    bindCharts();
  }

  function visibleSvg(figure) {
    var svgs = figure.querySelectorAll("svg");
    for (var i = 0; i < svgs.length; i++) {
      if (svgs[i].getClientRects().length) return svgs[i];
    }
    return null;
  }

  function svgScale(svg) {
    var rect = svg.getBoundingClientRect();
    var vb = svg.viewBox.baseVal;
    var scale = Math.min(rect.width / vb.width, rect.height / vb.height);
    return {
      scale: scale,
      ox: (rect.width - vb.width * scale) / 2,
      oy: (rect.height - vb.height * scale) / 2,
      rect: rect,
    };
  }

  function toViewBox(svg, clientX, clientY) {
    var s = svgScale(svg);
    return {
      x: (clientX - s.rect.left - s.ox) / s.scale,
      y: (clientY - s.rect.top - s.oy) / s.scale,
      scale: s.scale,
    };
  }

  function toPage(svg, figure, x, y) {
    var s = svgScale(svg);
    var fig = figure.getBoundingClientRect();
    return {
      x: s.rect.left - fig.left + s.ox + x * s.scale,
      y: s.rect.top - fig.top + s.oy + y * s.scale,
      scale: s.scale,
    };
  }

  function lowerBound(pts, x) {
    var lo = 0, hi = pts.length;
    while (lo < hi) {
      var mid = (lo + hi) >> 1;
      if (pts[mid].x < x) lo = mid + 1;
      else hi = mid;
    }
    return lo;
  }

  function nearestPoint(pts, at, radius) {
    var right = lowerBound(pts, at.x);
    var left = right - 1;
    var nearest = pts[right];
    if (!nearest || (left >= 0 && at.x - pts[left].x < nearest.x - at.x)) {
      nearest = pts[left];
    }
    if (!radius) return nearest;

    // Nearby line samples can overlap horizontally. Use both coordinates
    // within a few screen pixels so the point at the bottom of a dip wins
    // when the pointer is on it.
    var bestDistance = Infinity;
    for (var i = lowerBound(pts, at.x - radius); i < pts.length && pts[i].x <= at.x + radius; i++) {
      var dx = pts[i].x - at.x;
      var dy = pts[i].y - at.y;
      var distance = dx * dx + dy * dy;
      if (distance < bestDistance) {
        nearest = pts[i];
        bestDistance = distance;
      }
    }
    return nearest;
  }

  function hoverData(figure) {
    if (figure._hover !== undefined) return figure._hover;
    var raw = figure.getAttribute("data-hover");
    if (!raw) {
      figure._hover = null;
      return null;
    }
    try {
      figure._hover = JSON.parse(raw);
    } catch (e) {
      figure._hover = null;
    }
    return figure._hover;
  }

  function bindCharts() {
    var tip = document.getElementById("chart-tip");
    if (!tip) {
      tip = document.createElement("div");
      tip.id = "chart-tip";
      document.body.appendChild(tip);
    }
    tip.hidden = true;
    var figures = document.querySelectorAll("figure.chart[data-hover]");
    for (var i = 0; i < figures.length; i++) {
      bindChart(figures[i], tip);
    }
  }

  function bindChart(figure, tip) {
    var cross = figure.querySelector(".chart-cross");
    if (!cross) {
      cross = document.createElement("div");
      cross.className = "chart-cross";
      figure.appendChild(cross);
    }
    var dot = figure.querySelector(".chart-dot");
    if (!dot) {
      dot = document.createElement("div");
      dot.className = "chart-dot";
      figure.appendChild(dot);
    }
    var cell = figure.querySelector(".chart-cell");
    if (!cell) {
      cell = document.createElement("div");
      cell.className = "chart-cell";
      figure.appendChild(cell);
    }

    function hide() {
      tip.hidden = true;
      cross.style.display = "none";
      dot.style.display = "none";
      cell.style.display = "none";
    }

    figure.addEventListener("pointermove", function (event) {
      var data = hoverData(figure);
      var svg = visibleSvg(figure);
      if (!data || !data.pts || !data.pts.length || !svg) {
        hide();
        return;
      }
      var at = toViewBox(svg, event.clientX, event.clientY);
      var plot = data.p;
      if (at.x < plot[0] || at.x > plot[0] + plot[2] || at.y < plot[1] || at.y > plot[1] + plot[3]) {
        hide();
        return;
      }
      var pt;
      if (data.k === "heat") {
        var pitch = data.c || 11;
        var col = Math.floor((at.x - plot[0]) / pitch);
        var row = Math.floor((at.y - plot[1]) / pitch);
        if (col < 0 || col > 23 || row < 0 || row > 6) {
          hide();
          return;
        }
        pt = data.pts[row * 24 + col];
      } else {
        pt = nearestPoint(data.pts, at, data.k === "line" ? 4 / at.scale : 0);
      }
      if (!pt) {
        hide();
        return;
      }
      tip.hidden = false;
      tip.textContent = pt.l;
      var left = event.clientX + 12;
      var top = event.clientY + 16;
      if (left + tip.offsetWidth > window.innerWidth - 8) left = event.clientX - tip.offsetWidth - 12;
      if (top + tip.offsetHeight > window.innerHeight - 8) top = event.clientY - tip.offsetHeight - 12;
      tip.style.left = left + "px";
      tip.style.top = top + "px";

      var pos = toPage(svg, figure, pt.x, pt.y);
      var topLeft = toPage(svg, figure, plot[0], plot[1]);
      var bottom = toPage(svg, figure, plot[0], plot[1] + plot[3]);
      if (data.k === "heat") {
        cross.style.display = "none";
        dot.style.display = "none";
        cell.style.display = "block";
        cell.style.left = pos.x + "px";
        cell.style.top = pos.y + "px";
        cell.style.width = ((data.c || 11) - (data.g || 0)) * pos.scale + "px";
        cell.style.height = ((data.c || 11) - (data.g || 0)) * pos.scale + "px";
      } else {
        cell.style.display = "none";
        cross.style.display = "block";
        cross.style.left = pos.x + "px";
        cross.style.top = topLeft.y + "px";
        cross.style.height = (bottom.y - topLeft.y) + "px";
        dot.style.display = "block";
        dot.style.left = pos.x + "px";
        dot.style.top = pos.y + "px";
      }
    });
    figure.addEventListener("pointerleave", hide);
  }

  bind();
})();
