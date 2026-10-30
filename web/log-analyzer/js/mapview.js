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
 * @param {object} state   { worldSize, layers, background, selectedPlayer }
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
  const py = (north) => (north / world) * cssHeight;

  // ── background ───────────────────────────────────────────
  ctx.fillStyle = '#0a0d10';
  ctx.fillRect(0, 0, cssWidth, cssHeight);

  if (state.background) {
    ctx.globalAlpha = 0.9;
    try {
      ctx.drawImage(state.background, 0, 0, cssWidth, cssHeight);
    } catch {
      /* image not ready yet — grid is drawn next frame */
    }
    ctx.globalAlpha = 1;
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

  if (!adm) return;

  const layers = state.layers;

  // ── heatmap ──────────────────────────────────────────────
  if (layers.heat) {
    const grid = new Float32Array(HEAT_COLS * HEAT_ROWS);
    let max = 0;
    const addPoint = (east, north) => {
      const cx = Math.min(HEAT_COLS - 1, Math.max(0, Math.floor((east / world) * HEAT_COLS)));
      const cz = Math.min(HEAT_ROWS - 1, Math.max(0, Math.floor((north / world) * HEAT_ROWS)));
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
    const chosen = state.selectedPlayer
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
  if (state.selectedPlayer) {
    const p = adm.playerByKey.get(state.selectedPlayer);
    if (p?.lastKnownPos) {
      ctx.strokeStyle = 'rgba(255,255,255,0.8)';
      ctx.lineWidth = 1.5;
      ctx.beginPath();
      ctx.arc(px(p.lastKnownPos.x), py(p.lastKnownPos.y), 8, 0, Math.PI * 2);
      ctx.stroke();
    }
  }
}
