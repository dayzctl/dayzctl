/**
 * Map rendering on a plain 2D canvas.
 *
 * No tiles are shipped or downloaded: DayZ map imagery is copyrighted, so the
 * default is a coordinate grid, and you can load your own map image (e.g. an
 * iZurvive screenshot or the map_dayz_chernarus.jpg from your server files).
 *
 * DayZ world coordinates are metres: x = east, z = south, y = up.
 */

/**
 * World size inferred from the analysed positions, so a map without metadata
 * still scales sensibly.
 * @param {{maxX:number, maxY:number}|null|undefined} bounds
 */
export function inferWorldSize(bounds, fallback = 15360) {
  if (!bounds) return fallback;
  const extent = Math.max(Math.abs(bounds.maxX ?? 0), Math.abs(bounds.maxY ?? 0));
  if (!Number.isFinite(extent) || extent <= 0) return fallback;
  // Round up to the next kilometre and leave a small margin at the edge.
  const rounded = Math.ceil((extent * 1.02) / 1024) * 1024;
  return Math.min(40960, Math.max(1024, rounded));
}

const HEAT_COLS = 96;
const HEAT_ROWS = 96;

/** Hard ceiling for the canvas zoom, independent of the tile pyramid. */
export const MAX_MAP_ZOOM = 64;

/**
 * Resolves the viewport for a square canvas of `size` CSS pixels.
 *
 * `zoom` 1 means the whole map fits (the map can never be zoomed out past its
 * own size), and the centre is clamped so no empty space can be dragged into
 * view.
 *
 * @returns {{zoom:number, k:number, cx:number, cy:number}} k = CSS px per metre
 */
export function clampView(world, zoom, width, height, cx, cy) {
  const w = world > 0 ? world : 15360;
  const z = Math.min(MAX_MAP_ZOOM, Math.max(1, Number(zoom) || 1));
  // Zoom 1 has to show the whole map, so the scale follows the shorter side of
  // the canvas and the rest is letterboxed.
  const k = (Math.min(width, height) / w) * z;
  const axis = (c, size) => {
    const half = size / (2 * k);
    if (2 * half >= w) return w / 2;
    const v = Number.isFinite(c) ? c : w / 2;
    return Math.min(Math.max(v, half), w - half);
  };
  return { zoom: z, k, cx: axis(cx, width), cy: axis(cy, height) };
}

/* ── Tile imagery ──────────────────────────────────────────
 * The map service publishes a {z}/{x}/{y} pyramid: 2^z tiles per side,
 * tile (0,0) at the north-west corner, one tile = 256 px.
 */
const TILE_SIZE = 256;
const tileImages = new Map(); // "url|z|x|y" → { img, state }

/**
 * Zoom level to request for a canvas of `px` CSS pixels. One step finer than
 * "exactly covers the canvas" keeps the imagery crisp on retina screens.
 */
function tileZoom(px, maxZoom, minZoom) {
  const wanted = Math.ceil(Math.log2(Math.max(1, (px * 2) / TILE_SIZE)));
  return Math.max(minZoom || 1, Math.min(maxZoom || 7, wanted));
}

/**
 * Draws the tiles that intersect the viewport at a zoom level matching the
 * current scale, and repaints once one arrives.
 * @returns {boolean} false when nothing could be drawn yet
 */
function drawTiles(ctx, view, width, height, onTile) {
  const t = view.tiles;
  if (!t?.url) return false;

  const world = view.worldSize || 15360;
  const k = view.k;
  // One step finer than "exactly covers the canvas" keeps imagery crisp on
  // high-density screens; the pyramid itself may run out before the canvas does.
  const wanted = Math.ceil(Math.log2(Math.max(1, (world * k * 2) / TILE_SIZE)));
  const z = Math.max(t.minZoom || 1, Math.min(t.maxZoom || 7, wanted));
  const perSide = 2 ** z;
  const wpp = world / perSide; // world metres covered by one tile
  const span = wpp * k; // its size on the canvas

  // Only the tiles inside the viewport are requested, so panning stays cheap.
  const halfW = width / (2 * k);
  const halfH = height / (2 * k);
  const left = view.cx - halfW;
  const right = view.cx + halfW;
  const north = view.cy + halfH;
  const south = view.cy - halfH;
  const tx0 = Math.max(0, Math.floor(left / wpp));
  const tx1 = Math.min(perSide - 1, Math.floor(right / wpp));
  const ty0 = Math.max(0, Math.floor((world - north) / wpp));
  const ty1 = Math.min(perSide - 1, Math.floor((world - south) / wpp));

  let drawn = 0;
  for (let ty = ty0; ty <= ty1; ty++) {
    for (let tx = tx0; tx <= tx1; tx++) {
      const key = `${t.url}|${z}|${tx}|${ty}`;
      const cached = tileImages.get(key);

      if (cached?.ok) {
        const x = (tx * wpp - view.cx) * k + width / 2;
        const y = (view.cy - (world - ty * wpp)) * k + height / 2;
        ctx.drawImage(cached.img, x, y, span + 0.6, span + 0.6);
        drawn++;
        continue;
      }
      if (cached) continue; // failed earlier; do not retry endlessly

      tileImages.set(key, { img: null, ok: false, pending: true });
      const img = new Image();
      img.decoding = 'async';
      img.onload = () => {
        tileImages.set(key, { img, ok: true });
        onTile?.();
      };
      img.onerror = () => tileImages.set(key, { img: null, ok: false });
      img.src = t.url.replace('{z}', z).replace('{x}', tx).replace('{y}', ty);
    }
  }
  return drawn > 0 || perSide === 0;
}

/**
 * Place names, skipping labels that would overlap one already drawn.
 */
function drawNames(ctx, names, width, world, toX, toY) {
  if (!names?.length) return;
  ctx.save();
  ctx.font = '11px ui-sans-serif, system-ui, sans-serif';
  ctx.textAlign = 'center';
  ctx.textBaseline = 'middle';
  ctx.lineJoin = 'round';

  const placed = [];
  for (const n of names) {
    const x = toX(n.x);
    const y = toY(n.y);
    if (x < -40 || x > width + 40 || y < 8 || y > width + 8) continue;

    const w = ctx.measureText(n.name).width + 6;
    const box = [x - w / 2, y - 7, w, 14];
    if (placed.some((p) => !(box[0] > p[0] + p[2] || box[0] + box[2] < p[0]
      || box[1] > p[1] + p[3] || box[1] + box[3] < p[1]))) continue;
    placed.push(box);

    ctx.strokeStyle = 'rgba(0, 0, 0, 0.75)';
    ctx.lineWidth = 3;
    ctx.strokeText(n.name, x, y);
    ctx.fillStyle = 'rgba(232, 238, 244, 0.92)';
    ctx.fillText(n.name, x, y);
  }
  ctx.restore();
  void world;
}

/**
 * Nearest recorded position of `key` at or before `at`.
 * Positions are {x: east, y: north, z: altitude}, matching DayZ's pos=<x, y, z>.
 * @returns {{x:number,y:number}|null}
 */
function positionAt(player, at) {
  if (!player?.positions.length) return null;
  let best = null;
  for (const p of player.positions) {
    if (p.at === null) continue;
    if (p.at <= at) {
      if (!best || p.at > best.at) best = p;
    }
  }
  return best ? { x: best.x, y: best.y } : null;
}


// Breathing room left below the canvas so the card edge is visible.
const MAP_FOOTER_ALLOWANCE = 24;

/**
 * How tall the map may be before the page starts scrolling.
 *
 * The card's non-canvas part (toolbar above, legend and hint below) is taken as
 * the difference between the card and the canvas heights, so it stays correct
 * whatever the card contains. It is independent of the canvas height, which is
 * what makes a single measurement enough.
 */
function availableHeight(canvas) {
  const card = canvas.closest('.card') ?? canvas.parentElement;
  if (!card) return window.innerHeight;
  const overhead = Math.max(0, card.getBoundingClientRect().height - canvas.getBoundingClientRect().height);
  return Math.round(window.innerHeight - overhead - MAP_FOOTER_ALLOWANCE);
}

/**
 * @param {HTMLCanvasElement} canvas
 * @param {object} report analysis report
 * @param {object} state   { worldSize, layers, tiles, background, selectedKeys, windowFrom, windowTo, onTile }
 */
export function renderMap(canvas, report, state) {
  const adm = report?.adm;
  const ctx = canvas.getContext('2d');
  if (!ctx) return;

  // Fit the map to the space it can actually use: as wide as its card, but never
  // taller than the window allows once the controls above it are accounted for,
  // so neither the map nor the page has to scroll.
  const cssWidth = Math.max(200, Math.round(canvas.parentElement?.clientWidth ?? 640));
  const cssHeight = Math.max(200, Math.min(cssWidth, availableHeight(canvas)));
  const dpr = Math.min(window.devicePixelRatio || 1, 2);

  canvas.width = Math.round(cssWidth * dpr);
  canvas.height = Math.round(cssHeight * dpr);
  canvas.style.width = `${cssWidth}px`;
  canvas.style.height = `${cssHeight}px`;

  ctx.setTransform(dpr, 0, 0, dpr, 0, 0);
  ctx.clearRect(0, 0, cssWidth, cssHeight);

  const world = state.worldSize || 15360;
  // Zoom/pan: zoom 1 fits the whole map, so it cannot be zoomed out further.
  const vp = clampView(world, state.zoom, cssWidth, cssHeight, state.centerX, state.centerY);
  const k = vp.k;
  const x0 = vp.cx - cssWidth / (2 * k); // west edge, world metres
  const y0 = vp.cy + cssHeight / (2 * k); // north edge
  const px = (x) => (x - vp.cx) * k + cssWidth / 2;
  // DayZ's second coordinate grows north, so the canvas y axis is inverted:
  // the north edge of the map must end up at the top, like the imagery.
  const py = (north) => (vp.cy - north) * k + cssHeight / 2;
  // Exposed so the pointer handlers can turn pixel deltas into metres, and so
  // the applied viewport is visible in the DOM for debugging.
  state.viewport = { ...vp, world, width: cssWidth, height: cssHeight, left: x0, top: y0 };
  canvas.dataset.zoom = vp.zoom.toFixed(2);
  canvas.dataset.center = `${Math.round(vp.cx)},${Math.round(vp.cy)}`;

  // ── background ───────────────────────────────────────────
  ctx.fillStyle = '#0a0d10';
  ctx.fillRect(0, 0, cssWidth, cssHeight);

  const layers = state.layers || {};
  let haveImagery = false;

  if (state.background) {
    ctx.globalAlpha = 0.9;
    try {
      // The image covers the whole map, so it scales and shifts with the view.
      ctx.drawImage(state.background, px(0), py(world), world * k, world * k);
      haveImagery = true;
    } catch {
      /* image not ready yet — grid is drawn next frame */
    }
    ctx.globalAlpha = 1;
  } else if (layers.tiles !== false && drawTiles(ctx, { ...state, worldSize: world, k, cx: vp.cx, cy: vp.cy }, cssWidth, cssHeight, state.onTile)) {
    haveImagery = true;
  } else {
    // Coordinate grid, spaced so lines stay readable at any zoom.
    ctx.strokeStyle = 'rgba(120, 140, 160, 0.12)';
    ctx.lineWidth = 1;
    let stepM = 1024;
    while (stepM * k < 55) stepM *= 2;
    while (stepM * k > 220) stepM /= 2;
    const first = Math.floor(x0 / stepM) * stepM;
    for (let v = first; v <= x0 + cssWidth / k + stepM; v += stepM) {
      const gx = px(v);
      ctx.beginPath();
      ctx.moveTo(gx, 0);
      ctx.lineTo(gx, cssHeight);
      ctx.stroke();
    }
    const topM = y0;
    const bottomM = y0 - cssHeight / k;
    const firstY = Math.ceil(bottomM / stepM) * stepM;
    for (let v = firstY; v <= topM + stepM; v += stepM) {
      const gy = py(v);
      ctx.beginPath();
      ctx.moveTo(0, gy);
      ctx.lineTo(cssWidth, gy);
      ctx.stroke();
    }

    ctx.fillStyle = 'rgba(140, 160, 180, 0.35)';
    ctx.font = '10px ui-monospace, monospace';
    for (let v = firstY; v <= topM + stepM; v += stepM) {
      if (v < 0 || v > world) continue;
      ctx.fillText(`${Math.round(v / 100) / 10}km`, px(0) + 4, py(v) - 4);
    }
  }
  // ── place names ──────────────────────────────────────────
  if (haveImagery && layers.names !== false && state.tiles?.names?.length) {
    drawNames(ctx, state.tiles.names, cssWidth, world, px, py);
  }

  if (!adm) return;

  // ── timeline window ──────────────────────────────────────
  // Everything drawn from here on is limited to [windowFrom, windowTo] when a
  // range is supplied; events without a timestamp are only kept for the whole
  // period, since they cannot be placed inside a narrower one.
  const hasWindow = Number.isFinite(state.windowFrom) && Number.isFinite(state.windowTo);
  const inWindow = (at) => !hasWindow || (at !== null && at !== undefined && at >= state.windowFrom && at <= state.windowTo);

  // ── player scope ────────────────────────────────────────
  // With players ticked, every layer is built from them alone: heat, kills,
  // bases, last positions and jumps as well as the trails.
  const picked = state.selectedKeys?.length ? new Set(state.selectedKeys) : null;
  const involves = (...keys) => !picked || keys.some((k) => k && picked.has(k));

  const lastSampled = (p) => {
    if (!hasWindow) return p.lastKnownPos;
    for (let i = p.positions.length - 1; i >= 0; i--) {
      if (inWindow(p.positions[i].at)) return p.positions[i];
    }
    return null;
  };

  // ── heatmap ──────────────────────────────────────────────
  if (layers.heat) {
    const grid = new Float32Array(HEAT_COLS * HEAT_ROWS);
    let max = 0;
    const addPoint = (east, north) => {
      const cx = Math.min(HEAT_COLS - 1, Math.max(0, Math.floor((east / world) * HEAT_COLS)));
      // Row 0 is the north edge of the canvas.
      const cz = Math.min(HEAT_ROWS - 1, Math.max(0, Math.floor((1 - north / world) * HEAT_ROWS)));
      const idx = cz * HEAT_COLS + cx;
      grid[idx] += 1;
      if (grid[idx] > max) max = grid[idx];
    };

    for (const p of adm.players) {
      if (!involves(p.key)) continue;
      for (const s of p.positions) if (inWindow(s.at)) addPoint(s.x, s.y);
    }
    for (const b of adm.builds) if (b.pos && inWindow(b.at) && involves(b.playerKey)) addPoint(b.pos.x, b.pos.y);

    if (max > 0) {
      const cellW = world / HEAT_COLS;
      const cellH = world / HEAT_ROWS;
      for (let r = 0; r < HEAT_ROWS; r++) {
        for (let c = 0; c < HEAT_COLS; c++) {
          const v = grid[r * HEAT_COLS + c];
          if (!v) continue;
          const alpha = Math.min(0.72, (v / max) ** 0.55);
          ctx.fillStyle = `rgba(228, 92, 60, ${alpha})`;
          // Cell edges go through px/py so the heatmap zooms and pans with the
          // rest of the layers instead of staying pinned to the canvas.
          const gx = px(c * cellW);
          const gy = py(world - r * cellH);
          ctx.fillRect(gx, gy, cellW * k + 0.6, cellH * k + 0.6);
        }
      }
    }
  }

  // ── event dots ───────────────────────────────────────────
  // Every drawn event leaves a hoverable dot. Events landing on the same spot
  // share one dot, so the tooltip can list the players involved.
  const hotspots = new Map();
  const addHotspot = (pos, item) => {
    if (!pos) return;
    const key = `${Math.round(pos.x / 25)}:${Math.round(pos.y / 25)}`;
    const spot = hotspots.get(key) || { key, x: px(pos.x), y: py(pos.y), items: [] };
    spot.items.push(item);
    hotspots.set(key, spot);
  };

  // ── base building ────────────────────────────────────────
  if (layers.builds) {
    ctx.fillStyle = 'rgba(90, 200, 220, 0.85)';
    for (const b of adm.builds) {
      if (!b.pos || !inWindow(b.at) || !involves(b.playerKey)) continue;
      const x = px(b.pos.x);
      const y = py(b.pos.y);
      ctx.fillRect(x - 2.5, y - 2.5, 5, 5);
      addHotspot(b.pos, {
        kind: 'build',
        at: b.at,
        players: [b.player],
        text: `${b.kind === 'place' ? 'Placed' : 'Built'} ${b.object}`,
      });
    }
  }

  // ── connections ──────────────────────────────────────────
  if (layers.connects) {
    ctx.fillStyle = 'rgba(120, 190, 120, 0.55)';
    for (const p of adm.players) {
      if (!involves(p.key)) continue;
      const spot = lastSampled(p);
      if (!spot) continue;
      ctx.beginPath();
      ctx.arc(px(spot.x), py(spot.y), 2, 0, Math.PI * 2);
      ctx.fill();
    }
  }

  // ── trails ───────────────────────────────────────────────
  if (layers.trails) {
    const chosen = state.selectedKeys?.length
      ? adm.players.filter((p) => state.selectedKeys.includes(p.key)).slice(0, 40)
      : adm.players.slice(0, 12);

    chosen.forEach((p, i) => {
      const pts = p.positions.filter((s) => s.at !== null && s.at !== undefined && inWindow(s.at));
      if (pts.length < 2) return;
      ctx.beginPath();
      ctx.strokeStyle = `hsla(${(i * 47) % 360} 78% 62% / 0.75)`;
      // Round caps keep corners smooth; the width stays readable but does not
      // swallow the imagery when several trails overlap.
      ctx.lineWidth = 1.8;
      ctx.lineJoin = 'round';
      ctx.lineCap = 'round';
      pts.forEach((s, idx) => {
        const x = px(s.x);
        const y = py(s.y);
        if (idx === 0) ctx.moveTo(x, y);
        else ctx.lineTo(x, y);
      });
      ctx.stroke();
    });
  }

  // ── kills (shot lines) ───────────────────────────────────
  if (layers.kills) {
    for (const k of adm.kills) {
      // A kill is shown when either side of it is in the selection.
      if (!k.pos || !inWindow(k.at) || !involves(k.killerKey, k.victimKey)) continue;
      const vx = px(k.pos.x);
      const vy = py(k.pos.y);

      const killer = adm.playerByKey.get(k.killerKey);
      const from = k.at !== null ? positionAt(killer, k.at) : null;

      if (from) {
        ctx.strokeStyle = 'rgba(229, 72, 77, 0.45)';
        ctx.lineWidth = 1.2;
        ctx.beginPath();
        ctx.moveTo(px(from.x), py(from.y));
        ctx.lineTo(vx, vy);
        ctx.stroke();
      }

      ctx.fillStyle = 'rgba(229, 72, 77, 0.95)';
      ctx.beginPath();
      ctx.arc(vx, vy, 3, 0, Math.PI * 2);
      ctx.fill();

      addHotspot(k.pos, {
        kind: 'kill',
        at: k.at,
        players: [k.killer, k.victim],
        text: `${k.killer} killed ${k.victim}`,
      });
    }
  }

  // ── suspicious jumps ─────────────────────────────────────
  if (layers.jumps) {
    for (const s of adm.suspicious) {
      if (s.type !== 'teleport' || !s.from || !s.to || !inWindow(s.at) || !involves(s.playerKey)) continue;
      ctx.strokeStyle = 'rgba(217, 165, 59, 0.75)';
      ctx.setLineDash([4, 4]);
      ctx.lineWidth = 1.4;
      ctx.beginPath();
      ctx.moveTo(px(s.from.x), py(s.from.y));
      ctx.lineTo(px(s.to.x), py(s.to.y));
      ctx.stroke();
      ctx.setLineDash([]);

      ctx.fillStyle = 'rgba(217, 165, 59, 0.9)';
      for (const end of [s.from, s.to]) {
        ctx.beginPath();
        ctx.arc(px(end.x), py(end.y), 2.5, 0, Math.PI * 2);
        ctx.fill();
      }
      addHotspot(s.to, {
        kind: 'jump',
        at: s.at,
        players: [s.player],
        text: `Impossible movement — ${s.detail}`,
      });
    }
  }

  // The dot under the pointer gets a ring, drawn last so nothing covers it.
  state.hotspots = [...hotspots.values()];
  const hovered = state.hoverKey ? hotspots.get(state.hoverKey) : null;
  if (hovered) {
    ctx.strokeStyle = 'rgba(255, 255, 255, 0.9)';
    ctx.lineWidth = 1.6;
    ctx.beginPath();
    ctx.arc(hovered.x, hovered.y, 9, 0, Math.PI * 2);
    ctx.stroke();
  }

  // ── selection highlight ──────────────────────────────────
  const highlighted = state.selectedKeys?.length ? state.selectedKeys : [];
  for (const key of highlighted) {
    const p = adm.playerByKey.get(key);
    const spot = p ? lastSampled(p) : null;
    if (!spot) continue;
    ctx.strokeStyle = 'rgba(255,255,255,0.8)';
    ctx.lineWidth = 1.5;
    ctx.beginPath();
    ctx.arc(px(spot.x), py(spot.y), 8, 0, Math.PI * 2);
    ctx.stroke();
  }
}
