/**
 * Map rendering on a plain 2D canvas.
 *
 * No tiles are shipped or downloaded: DayZ map imagery is copyrighted, so the
 * default is a coordinate grid, and you can load your own map image (e.g. an
 * iZurvive screenshot or the map_dayz_chernarus.jpg from your server files).
 *
 * DayZ world coordinates are metres: x = east, z = south, y = up.
 */

// Only maps with a stable, well-known world size carry a fixed value. Terrains
// like Nasdara are rebuilt by their authors, so their size is inferred from the
// analysed positions (worldSize: 0) and stays editable.
export const MAPS = [
  { id: 'auto', label: 'Auto (from log positions)', worldSize: 0 },
  { id: 'chernarus', label: 'Chernarus', worldSize: 15360 },
  { id: 'livonia', label: 'Livonia', worldSize: 15360 },
  { id: 'sakhal', label: 'Sakhal', worldSize: 12800 },
  { id: 'namalsk', label: 'Namalsk', worldSize: 12800 },
  { id: 'nasdara', label: 'Nasdara', worldSize: 0 },
  { id: 'custom', label: 'Custom / other', worldSize: 0 },
];

/**
 * World size inferred from the analysed positions, so custom maps (Nasdara,
 * Deer Isle, …) are scaled correctly without being known in advance.
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
 * Draws every tile of the chosen zoom level, and repaints once a tile arrives.
 * @returns {boolean} false when nothing could be drawn yet
 */
function drawTiles(ctx, view, width, height, onTile) {
  const t = view.tiles;
  if (!t?.url) return false;

  const z = tileZoom(width, t.maxZoom, t.minZoom);
  const perSide = 2 ** z;
  const srcSize = perSide * TILE_SIZE; // source pixels covering the world
  const scale = width / srcSize;
  let drawn = 0;

  for (let ty = 0; ty < perSide; ty++) {
    for (let tx = 0; tx < perSide; tx++) {
      const key = `${t.url}|${z}|${tx}|${ty}`;
      const cached = tileImages.get(key);

      if (cached?.ok) {
        ctx.drawImage(cached.img, tx * TILE_SIZE * scale, ty * TILE_SIZE * scale,
          TILE_SIZE * scale + 0.6, TILE_SIZE * scale + 0.6);
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

/**
 * @param {HTMLCanvasElement} canvas
 * @param {object} report analysis report
 * @param {object} state   { worldSize, layers, tiles, background, selectedPlayer, onTile }
 */
export function renderMap(canvas, report, state) {
  const adm = report?.adm;
  const ctx = canvas.getContext('2d');
  if (!ctx) return;

  const cssWidth = Math.max(280, Math.round(canvas.parentElement?.clientWidth ?? 640));
  const cssHeight = cssWidth;
  const dpr = Math.min(window.devicePixelRatio || 1, 2);

  canvas.width = Math.round(cssWidth * dpr);
  canvas.height = Math.round(cssHeight * dpr);
  canvas.style.width = `${cssWidth}px`;
  canvas.style.height = `${cssHeight}px`;

  ctx.setTransform(dpr, 0, 0, dpr, 0, 0);
  ctx.clearRect(0, 0, cssWidth, cssHeight);

  const world = state.worldSize || 15360;
  const px = (x) => (x / world) * cssWidth;
  // DayZ's second coordinate grows north, so the canvas y axis is inverted:
  // the north edge of the map must end up at the top, like the imagery.
  const py = (north) => (1 - north / world) * cssHeight;

  // ── background ───────────────────────────────────────────
  ctx.fillStyle = '#0a0d10';
  ctx.fillRect(0, 0, cssWidth, cssHeight);

  const layers = state.layers || {};
  let haveImagery = false;

  if (state.background) {
    ctx.globalAlpha = 0.9;
    try {
      ctx.drawImage(state.background, 0, 0, cssWidth, cssHeight);
      haveImagery = true;
    } catch {
      /* image not ready yet — grid is drawn next frame */
    }
    ctx.globalAlpha = 1;
  } else if (layers.tiles !== false && drawTiles(ctx, state, cssWidth, cssHeight, state.onTile)) {
    haveImagery = true;
  } else {
    // Coordinate grid, every 1 km.
    ctx.strokeStyle = 'rgba(120, 140, 160, 0.12)';
    ctx.lineWidth = 1;
    const stepKm = world / 15; // ~1 km for 15 km maps
    for (let v = 0; v <= world; v += stepKm) {
      const gx = px(v);
      const gy = py(v);
      ctx.beginPath();
      ctx.moveTo(gx, 0);
      ctx.lineTo(gx, cssHeight);
      ctx.stroke();
      ctx.beginPath();
      ctx.moveTo(0, gy);
      ctx.lineTo(cssWidth, gy);
      ctx.stroke();
    }

    ctx.fillStyle = 'rgba(140, 160, 180, 0.35)';
    ctx.font = '10px ui-monospace, monospace';
    for (let v = 0; v <= world; v += stepKm * 3) {
      ctx.fillText(`${Math.round(v / 100) / 10}km`, px(v) + 3, 11);
      ctx.fillText(`${Math.round(v / 100) / 10}km`, 3, py(v) + 11);
    }
  }
  // ── place names ──────────────────────────────────────────
  if (haveImagery && layers.names !== false && state.tiles?.names?.length) {
    drawNames(ctx, state.tiles.names, cssWidth, world, px, py);
  }

  if (!adm) return;

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

    for (const p of adm.players) for (const s of p.positions) addPoint(s.x, s.y);
    for (const b of adm.builds) if (b.pos) addPoint(b.pos.x, b.pos.y);

    if (max > 0) {
      const cw = cssWidth / HEAT_COLS;
      const ch = cssHeight / HEAT_ROWS;
      for (let r = 0; r < HEAT_ROWS; r++) {
        for (let c = 0; c < HEAT_COLS; c++) {
          const v = grid[r * HEAT_COLS + c];
          if (!v) continue;
          const alpha = Math.min(0.72, (v / max) ** 0.55);
          ctx.fillStyle = `rgba(228, 92, 60, ${alpha})`;
          ctx.fillRect(c * cw, r * ch, cw + 0.6, ch + 0.6);
        }
      }
    }
  }

  // ── base building ────────────────────────────────────────
  if (layers.builds) {
    ctx.fillStyle = 'rgba(90, 200, 220, 0.85)';
    for (const b of adm.builds) {
      if (!b.pos) continue;
      const x = px(b.pos.x);
      const y = py(b.pos.y);
      ctx.fillRect(x - 2.5, y - 2.5, 5, 5);
    }
  }

  // ── connections ──────────────────────────────────────────
  if (layers.connects) {
    ctx.fillStyle = 'rgba(120, 190, 120, 0.55)';
    for (const p of adm.players) {
      if (!p.lastKnownPos) continue;
      ctx.beginPath();
      ctx.arc(px(p.lastKnownPos.x), py(p.lastKnownPos.y), 2, 0, Math.PI * 2);
      ctx.fill();
    }
  }

  // ── trails ───────────────────────────────────────────────
  if (layers.trails) {
    const chosen = state.selectedKeys?.length
      ? adm.players.filter((p) => state.selectedKeys.includes(p.key))
      : state.selectedPlayer
        ? adm.players.filter((p) => p.key === state.selectedPlayer)
        : adm.players.slice(0, 12);

    chosen.forEach((p, i) => {
      const pts = p.positions.filter((s) => s.at !== null);
      if (pts.length < 2) return;
      ctx.beginPath();
      ctx.strokeStyle = `hsla(${(i * 47) % 360} 72% 60% / 0.65)`;
      ctx.lineWidth = 1.6;
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
      if (!k.pos) continue;
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
    }
  }

  // ── suspicious jumps ─────────────────────────────────────
  if (layers.jumps) {
    for (const s of adm.suspicious) {
      if (s.type !== 'teleport' || !s.from || !s.to) continue;
      ctx.strokeStyle = 'rgba(217, 165, 59, 0.75)';
      ctx.setLineDash([4, 4]);
      ctx.lineWidth = 1.4;
      ctx.beginPath();
      ctx.moveTo(px(s.from.x), py(s.from.y));
      ctx.lineTo(px(s.to.x), py(s.to.y));
      ctx.stroke();
      ctx.setLineDash([]);
    }
  }

  // ── selection highlight ──────────────────────────────────
  const highlighted = state.selectedKeys?.length
    ? state.selectedKeys
    : state.selectedPlayer ? [state.selectedPlayer] : [];
  for (const key of highlighted) {
    const p = adm.playerByKey.get(key);
    if (!p?.lastKnownPos) continue;
    ctx.strokeStyle = 'rgba(255,255,255,0.8)';
    ctx.lineWidth = 1.5;
    ctx.beginPath();
    ctx.arc(px(p.lastKnownPos.x), py(p.lastKnownPos.y), 8, 0, Math.PI * 2);
    ctx.stroke();
  }
}
