/**
 * Map rendering on a plain 2D canvas.
 *
 * The basemap is the bundled topographic imagery of the DayZ map (a drawn road
 * map), plus place names from the map metadata and the event overlay; without
 * any imagery a coordinate grid is drawn, and a custom picture can be loaded
 * instead.
 *
 * DayZ world coordinates are metres: x = east, z = south, y = up.
 */

import { colorFor } from './format.js';

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

/* ── heatmap ramp ──────────────────────────────────────────
 * Density is drawn as tinted heat (orange → red → deep crimson) with normal
 * alpha blending: additive light would wash out on the pale desert imagery,
 * while a saturated tint reads on both the light and the dark maps.
 */
const HEAT_RAMP = [
  [0.0, 255, 110, 40],
  [0.45, 240, 62, 40],
  [0.75, 204, 26, 62],
  [1.0, 132, 8, 44],
];

/** Colour of the heat ramp at `t` (0 = coldest, 1 = hottest). */
function heatColor(t) {
  const v = Math.min(1, Math.max(0, t));
  for (let i = 1; i < HEAT_RAMP.length; i++) {
    const [p1, r1, g1, b1] = HEAT_RAMP[i];
    if (v <= p1) {
      const [p0, r0, g0, b0] = HEAT_RAMP[i - 1];
      const f = p1 === p0 ? 0 : (v - p0) / (p1 - p0);
      return [r0 + (r1 - r0) * f, g0 + (g1 - g0) * f, b0 + (b1 - b0) * f];
    }
  }
  return HEAT_RAMP.at(-1).slice(1);
}

/** Hard ceiling for the canvas zoom, independent of the tile pyramid. */
export const MAX_MAP_ZOOM = 64;

/**
 * Resolves the viewport for a canvas of `width` × `height` CSS pixels.
 *
 * Zoom 1 fits the map's **width** to the canvas, so a wide panel is filled edge
 * to edge instead of letterboxing a small square in it; the surplus height is
 * cropped and can be panned. Zooming in is allowed, zooming out is not — 1 is
 * the floor, so there is never a band of empty space at the sides.
 *
 * @returns {{zoom:number, k:number, cx:number, cy:number, minZoom:number}}
 *          k = CSS px per metre
 */
export function clampView(world, zoom, width, height, cx, cy) {
  const w = world > 0 ? world : 15360;
  const z = Math.min(MAX_MAP_ZOOM, Math.max(1, Number(zoom) || 1));
  // One world width maps to one canvas width at zoom 1.
  const k = (width / w) * z;
  const axis = (c, size) => {
    const half = size / (2 * k);
    if (2 * half >= w) return w / 2;
    const v = Number.isFinite(c) ? c : w / 2;
    return Math.min(Math.max(v, half), w - half);
  };
  return { zoom: z, k, cx: axis(cx, width), cy: axis(cy, height), minZoom: 1 };
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

/* ── event markers ──────────────────────────────────────────
 * Events are drawn in screen space: a dark halo, a coloured core and a light
 * rim, so a dot stays legible on the dark satellite imagery at any zoom. Keep
 * the colours in step with the legend in ui.js.
 */
const MARKER = {
  kill: { color: '#e5484d', size: 6.5 },
  build: { color: '#5ac8dc', size: 6, shape: 'square' },
  // Deployables (bear trap, land mine, tripwire) are build events, but they are
  // not structures, so they get their own shape and colour.
  trap: { color: '#d46bd8', size: 6, shape: 'square' },
  // Where a player died. A triangle so it reads differently from a kill dot.
  death: { color: '#dfe5ec', size: 7, shape: 'triangle' },
  jump: { color: '#d9a53b', size: 6 },
  // Where an impossible movement started: a smaller dot in the same colour.
  jumpOrigin: { color: '#d9a53b', size: 4.6 },
  // Last position / connection: an event dot like the rest, never hidden by
  // the trail it sits on.
  connect: { color: '#78be78', size: 5.5 },
  // The player's last recorded spot: same dot, different meaning for the tally.
  last: { color: '#78be78', size: 5.5 },
  // Logins layer: where a session started and where it ended. The logout is a
  // ring wide enough to stay visible around the login dot it usually shares a
  // position with.
  login: { color: '#4cc38a', size: 5.6 },
  logout: { color: '#a7b4c4', size: 8, shape: 'ring' },
  // A disconnect that came with a kick/ban line: red ring, same shape as a
  // normal logout.
  logoutKick: { color: '#e5484d', size: 8, shape: 'ring' },
  // One infected attack: a run of hits from zombies at nearly the same spot.
  infected: { color: '#e0803f', size: 6 },
  infectedSwarm: { color: '#e0803f', size: 8.5 },
};

/** Events a dedicated layer already turns into a hoverable marker. When that
 *  layer is off, the position dot has to explain itself instead — otherwise the
 *  emote on a logout spot would be the only thing a tooltip reports. */
function coveredByLayer(kind, layers) {
  switch (kind) {
    case 'connect':
    case 'disconnect':
      return Boolean(layers.logins);
    case 'kill':
      return Boolean(layers.kills);
    case 'build':
    case 'place':
    case 'trap':
      return Boolean(layers.builds);
    case 'death':
      return Boolean(layers.deaths);
    case 'jump':
      return Boolean(layers.jumps);
    default:
      return false;
  }
}

/** Hits from the same source this close in time and space belong to one attack. */
const ATTACK_LINK_SECONDS = 90;
const ATTACK_LINK_METRES = 120;

/** Trail stroke width in screen pixels: thicker than before, and thicker still
 *  as the map is zoomed in, but always narrower than a marker's diameter so
 *  every event dot stays visible on top of the line. */
function trailWidth(zoom) {
  return Math.min(6, 3.4 * Math.max(1, Math.sqrt(zoom || 1)));
}

function drawMarker(ctx, x, y, kind) {
  const m = MARKER[kind] || MARKER.kill;
  const path = (r) => {
    ctx.beginPath();
    if (m.shape === 'square') ctx.rect(x - r, y - r, r * 2, r * 2);
    else if (m.shape === 'triangle') {
      ctx.moveTo(x, y - r * 1.15);
      ctx.lineTo(x + r, y + r * 0.8);
      ctx.lineTo(x - r, y + r * 0.8);
      ctx.closePath();
    } else ctx.arc(x, y, r, 0, Math.PI * 2);
  };

  // A ring marks an ending (a logout): stroked only, so a login dot underneath
  // it keeps showing through.
  if (m.shape === 'ring') {
    ctx.save();
    path(m.size);
    ctx.lineCap = 'butt';
    ctx.lineWidth = 4;
    ctx.strokeStyle = 'rgba(4, 7, 10, 0.7)';
    ctx.stroke();
    path(m.size);
    ctx.lineWidth = 2.2;
    ctx.strokeStyle = m.color;
    ctx.stroke();
    ctx.restore();
    return;
  }

  // Halo first: it separates the dot from whatever is underneath it.
  path(m.size + 2);
  ctx.fillStyle = 'rgba(4, 7, 10, 0.65)';
  ctx.fill();

  path(m.size);
  ctx.fillStyle = m.color;
  ctx.fill();
  ctx.lineWidth = 1.2;
  ctx.strokeStyle = 'rgba(255, 255, 255, 0.92)';
  ctx.stroke();
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

  // The frame takes the full width of its card; its height is what it always
  // was (never taller than the width, never taller than the window allows).
  // The map then scales to that width, so a wide window gets a big map instead
  // of a small square with empty space on the sides.
  const cssWidth = Math.max(240, Math.round(canvas.parentElement?.clientWidth ?? 640));
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
  // A resize can clamp the zoom (the fit scale follows the canvas shape), so
  // the caller gets the viewport that was actually drawn.
  if (typeof state.onViewport === 'function') state.onViewport(state.viewport);

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
  } else if (drawTiles(ctx, { ...state, worldSize: world, k, cx: vp.cx, cy: vp.cy }, cssWidth, cssHeight, state.onTile)) {
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
  if (haveImagery && state.tiles?.names?.length) {
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
  // A density field rather than a grid of coloured squares: every sample drops a
  // soft radial blob and the blobs are added together, so busy places glow from
  // ember to a white core instead of showing large blocks. The grid lives in
  // screen space, so the field stays smooth while zooming and panning.
  if (layers.heat) {
    const cell = Math.max(9, Math.min(16, Math.round(cssWidth / 90)));
    const cols = Math.ceil(cssWidth / cell) + 1;
    const rows = Math.ceil(cssHeight / cell) + 1;
    const grid = new Float32Array(cols * rows);
    const add = (east, north) => {
      const c = Math.floor(px(east) / cell);
      const r = Math.floor(py(north) / cell);
      if (c < 0 || c >= cols || r < 0 || r >= rows) return;
      grid[r * cols + c] += 1;
    };

    for (const p of adm.players) {
      if (!involves(p.key)) continue;
      for (const s of p.positions) if (inWindow(s.at)) add(s.x, s.y);
    }
    for (const b of adm.builds) if (b.pos && inWindow(b.at) && involves(b.playerKey)) add(b.pos.x, b.pos.y);

    // Contrast reference: the busiest cell, softened to the 97th percentile so a
    // single camping spot does not wash every other hotspot out.
    const values = [];
    for (const v of grid) if (v > 0) values.push(v);
    if (values.length) {
      values.sort((a, b) => a - b);
      const ref = Math.max(1, values[Math.min(values.length - 1, Math.floor(values.length * 0.97))]);
      const radius = cell * 2.6;
      const shades = [];
      for (let i = 0; i <= 12; i++) {
        const t = i / 12;
        const [r0, g0, b0] = heatColor(t);
        const rgb = `${Math.round(r0)}, ${Math.round(g0)}, ${Math.round(b0)}`;
        const alpha = 0.09 + 0.5 * t ** 1.5;
        const grad = ctx.createRadialGradient(0, 0, 0, 0, 0, radius);
        grad.addColorStop(0, `rgba(${rgb}, ${alpha.toFixed(3)})`);
        grad.addColorStop(0.5, `rgba(${rgb}, ${(alpha * 0.55).toFixed(3)})`);
        grad.addColorStop(1, `rgba(${rgb}, 0)`);
        shades.push(grad);
      }

      ctx.save();
      for (let r = 0; r < rows; r++) {
        for (let c = 0; c < cols; c++) {
          const v = grid[r * cols + c];
          if (!v) continue;
          const t = Math.min(1, (v / ref) ** 0.6);
          const cx = c * cell + cell / 2;
          const cy = r * cell + cell / 2;
          // The gradient is built at the origin once; translating the canvas puts
          // it over the cell without allocating a gradient per cell.
          ctx.translate(cx, cy);
          ctx.fillStyle = shades[Math.min(12, Math.round(t * 12))];
          ctx.fillRect(-radius, -radius, radius * 2, radius * 2);
          ctx.translate(-cx, -cy);
        }
      }
      ctx.restore();
    }
  }

  // ── event dots ───────────────────────────────────────────
  // Every drawn event leaves a hoverable dot. Events landing on the same spot
  // share one dot, so the tooltip can list the players involved.
  const hotspots = new Map();
  const addHotspot = (pos, item) => {
    if (!pos) return;
    const x = px(pos.x);
    const y = py(pos.y);
    // Group in screen space, not in metres: dots that would overlap at the
    // current zoom become one dot that lists everything that happened there.
    const key = `${Math.round(x / 8)}:${Math.round(y / 8)}`;
    const size = (MARKER[item.kind] || MARKER.kill).size;
    const spot = hotspots.get(key) || { key, x, y, size, items: [] };
    spot.items.push(item);
    hotspots.set(key, spot);
  };

  // ── trails ───────────────────────────────────────────────
  // Drawn before every marker layer: the trail is the backdrop, the event dots
  // sit on top of it so a kill, build, jump or last position stays visible even
  // where the line runs straight through it.
  if (layers.trails) {
    const chosen = state.selectedKeys?.length
      ? adm.players.filter((p) => state.selectedKeys.includes(p.key)).slice(0, 40)
      : adm.players.slice(0, 12);
    const width = trailWidth(vp.zoom);
    // Event dots are always wider than the line they sit on, so a recorded
    // event never disappears under its own trail.
    const dotRadius = Math.max(2.4, width / 2 + 1.6);

    chosen.forEach((p) => {
      const pts = p.positions.filter((s) => s.at !== null && s.at !== undefined && inWindow(s.at));
      if (!pts.length) return;
      // Same colour as the player's row in the picker and the tables, so a
      // trail can be matched to a name without a legend.
      const color = colorFor(p.name, 0.8);
      ctx.strokeStyle = color;
      // Round caps keep corners smooth; the width stays readable but does not
      // swallow the imagery when several trails overlap.
      ctx.lineWidth = width;
      ctx.lineJoin = 'round';
      ctx.lineCap = 'round';

      // One path per segment: a recorded break (respawn, reconnect) starts a new
      // one, so the trail never draws a straight line across the map.
      let segment = [];
      const flush = () => {
        if (segment.length > 1) {
          ctx.beginPath();
          segment.forEach((s, idx) => {
            const x = px(s.x);
            const y = py(s.y);
            if (idx === 0) ctx.moveTo(x, y);
            else ctx.lineTo(x, y);
          });
          ctx.stroke();
        }
        segment = [];
      };
      for (const s of pts) {
        if (s.break && segment.length) flush();
        segment.push(s);
      }
      flush();

      // ── infected attacks ──
      // A zombie attack is a burst of hits (sometimes dozens in one second),
      // all from the same source at nearly the same place. Grouping them into
      // one dot keeps the map readable and lets the tooltip tell the story:
      // "7 infected hits · 34 damage".
      const grouped = new Set();
      const attacks = [];
      let running = null;
      for (const s of pts) {
        if (s.kind !== 'hit' || !/infected/i.test(s.source || '')) {
          running = null;
          continue;
        }
        if (running) {
          const prev = running[running.length - 1];
          const dt = (s.at ?? 0) - (prev.at ?? 0);
          const away = Math.hypot(s.x - prev.x, s.y - prev.y);
          if (dt <= ATTACK_LINK_SECONDS && away <= ATTACK_LINK_METRES) {
            running.push(s);
            continue;
          }
        }
        running = [s];
        attacks.push(running);
      }
      attacks.forEach((group) => group.forEach((s) => grouped.add(s)));

      // A dot on every recorded position: ADM only logs event positions, so the
      // dots are what the trail actually joins, and each one marks where an
      // event happened. Strided on very long trails to keep the repaint cheap.
      const stride = Math.max(1, Math.ceil(pts.length / 600));
      ctx.fillStyle = colorFor(p.name, 0.95);
      const dot = (s) => {
        ctx.beginPath();
        ctx.arc(px(s.x), py(s.y), dotRadius, 0, Math.PI * 2);
        ctx.fill();
      };
      for (let i = 0; i < pts.length; i += stride) {
        if (!grouped.has(pts[i])) dot(pts[i]);
      }
      if ((pts.length - 1) % stride !== 0 && !grouped.has(pts[pts.length - 1])) {
        dot(pts[pts.length - 1]);
      }

      for (const group of attacks) {
        const n = group.length;
        const cx = group.reduce((sum, s) => sum + s.x, 0) / n;
        const cy = group.reduce((sum, s) => sum + s.y, 0) / n;
        const damage = group.reduce((sum, s) => sum + (s.damage || 0), 0);
        drawMarker(ctx, px(cx), py(cy), n > 3 ? 'infectedSwarm' : 'infected');
        addHotspot(
          { x: cx, y: cy },
          {
            kind: 'infected',
            at: group[n - 1].at ?? null,
            players: [p.name],
            text: `${n} infected hit${n === 1 ? '' : 's'}${damage > 0 ? ` · ${Math.round(damage)} damage` : ''}`,
          },
        );
      }

      // Dots without a layer of their own (hits, emotes, chat, deaths) name the
      // event that happened there when hovered.
      for (const s of pts) {
        if (!s.kind || grouped.has(s) || coveredByLayer(s.kind, layers)) continue;
        addHotspot(
          { x: s.x, y: s.y },
          {
            kind: s.kind,
            at: s.at ?? null,
            players: [p.name],
            text: s.text || s.kind,
          },
        );
      }
    });
  }

  // ── logins ───────────────────────────────────────────────
  // A dot wherever a session started or ended: green for a connect, grey for a
  // disconnect. Reading a trail against these dots shows which part of the line
  // belongs to which session.
  if (layers.logins) {
    for (const p of adm.players) {
      if (!involves(p.key)) continue;
      for (const s of p.positions) {
        if (s.kind !== 'connect' && s.kind !== 'disconnect') continue;
        if (!inWindow(s.at)) continue;
        const login = s.kind === 'connect';
        const kicked = !login && s.kicked;
        drawMarker(ctx, px(s.x), py(s.y), login ? 'login' : kicked ? 'logoutKick' : 'logout');
        addHotspot(
          { x: s.x, y: s.y },
          {
            kind: login ? 'login' : kicked ? 'logoutKick' : 'logout',
            at: s.at ?? null,
            players: [p.name],
            text: `${p.name} ${s.text || (login ? 'connected' : 'disconnected')}`,
          },
        );
      }
    }
  }

  // ── base building ────────────────────────────────────────
  if (layers.builds) {
    for (const b of adm.builds) {
      if (!b.pos || !inWindow(b.at) || !involves(b.playerKey)) continue;
      // Traps are deployed, not built: same layer, different marker, so a bear
      // trap is not mistaken for a wall.
      const isTrap = Boolean(b.trap);
      drawMarker(ctx, px(b.pos.x), py(b.pos.y), isTrap ? 'trap' : 'build');
      addHotspot(b.pos, {
        kind: isTrap ? 'trap' : 'build',
        at: b.at,
        players: [b.player],
        text: isTrap ? `Trap: ${b.object}` : `${b.kind === 'place' ? 'Placed' : 'Built'} ${b.object}`,
      });
    }
  }

  // ── last positions ───────────────────────────────────────
  // A full marker rather than a bare arc, so it reads as an event dot at that
  // place and wins against the trail underneath it.
  if (layers.connects) {
    for (const p of adm.players) {
      if (!involves(p.key)) continue;
      const spot = lastSampled(p);
      if (!spot) continue;
      drawMarker(ctx, px(spot.x), py(spot.y), 'connect');
      // The newest sample is usually the join or the logout line itself. Naming
      // it "last position" would hide that event and, with the Logins layer on,
      // say the same thing twice — so it keeps the event's own verb.
      const sampleKinds = spot.kind === 'connect' || spot.kind === 'disconnect';
      if (sampleKinds && layers.logins) continue;
      const label = spot.kind ? spot.text || spot.kind : 'last position';
      addHotspot(spot, {
        kind: 'last',
        at: spot.at ?? null,
        players: [p.name],
        text: `${p.name} ${label}`,
      });
    }
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

      drawMarker(ctx, vx, vy, 'kill');

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

      // Both ends are event dots: the origin is smaller, the destination is
      // the hoverable marker with the details.
      drawMarker(ctx, px(s.from.x), py(s.from.y), 'jumpOrigin');
      drawMarker(ctx, px(s.to.x), py(s.to.y), 'jump');
      addHotspot(s.to, {
        kind: 'jump',
        at: s.at,
        players: [s.player],
        text: `Impossible movement — ${s.detail}`,
      });
    }
  }

  // ── deaths ───────────────────────────────────────────────
  // Every place a player died. ADM records PvP kills and environmental deaths in
  // different lists, so both are folded into one layer.
  if (layers.deaths) {
    for (const k of adm.kills) {
      if (!k.pos || !inWindow(k.at) || !involves(k.killerKey, k.victimKey)) continue;
      drawMarker(ctx, px(k.pos.x), py(k.pos.y), 'death');
      addHotspot(k.pos, {
        kind: 'death',
        at: k.at,
        players: [k.victim, k.killer],
        text: `${k.victim} died — killed by ${k.killer}`,
      });
    }
    for (const d of adm.deaths) {
      if (!d.pos || !inWindow(d.at) || !involves(d.playerKey)) continue;
      drawMarker(ctx, px(d.pos.x), py(d.pos.y), 'death');
      addHotspot(d.pos, {
        kind: 'death',
        at: d.at,
        players: [d.player],
        text: `${d.player} died${d.cause ? ` — ${d.cause}` : ''}`,
      });
    }
  }

  // The dot under the pointer gets a ring, drawn last so nothing covers it.
  state.hotspots = [...hotspots.values()];
  const hovered = state.hoverKey ? hotspots.get(state.hoverKey) : null;
  if (hovered) {
    ctx.lineWidth = 2;
    ctx.strokeStyle = 'rgba(255, 255, 255, 0.95)';
    ctx.beginPath();
    ctx.arc(hovered.x, hovered.y, (hovered.size || 5) + 5, 0, Math.PI * 2);
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
    ctx.arc(px(spot.x), py(spot.y), 10, 0, Math.PI * 2);
    ctx.stroke();
  }
}
