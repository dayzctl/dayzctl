/**
 * Thin client for the dayzctl web API.
 *
 * All parsing happens server-side; this module only fetches and normalises
 * the report so the UI can render it directly.
 */

const JSON_HEADERS = { Accept: 'application/json' };

async function requestJSON(path, options = {}) {
  let response;
  try {
    response = await fetch(path, { ...options, headers: { ...JSON_HEADERS, ...(options.headers || {}) } });
  } catch (err) {
    throw new Error(`Cannot reach the dayzctl API (${err.message}). Is \`dayzctl web\` running?`);
  }

  const text = await response.text();
  let payload = null;
  if (text) {
    try {
      payload = JSON.parse(text);
    } catch {
      payload = null;
    }
  }

  if (!response.ok) {
    // The login switch is on and the session ran out: send the browser back to
    // the sign-in form rather than showing a dead page.
    if (response.status === 401 && !path.startsWith('/api/health')) {
      window.location.assign('/login?next=' + encodeURIComponent(window.location.pathname + window.location.search));
    }
    const message = payload?.error || `${response.status} ${response.statusText}`;
    throw new Error(message);
  }
  return payload;
}

/** @returns {Promise<{status: string, version: string, instances: number}>} */
export function fetchHealth() {
  return requestJSON('/api/health');
}

/** @returns {Promise<Array<object>>} */
export async function fetchInstances() {
  const payload = await requestJSON('/api/instances');
  return payload?.instances ?? [];
}

/** @returns {Promise<{instance: string, profilesDir: string, files: Array<object>}>} */
export async function fetchLogs(instance) {
  const payload = await requestJSON(`/api/instances/${encodeURIComponent(instance)}/logs`);
  return {
    instance: payload?.instance ?? instance,
    profilesDir: payload?.profilesDir ?? '',
    files: payload?.files ?? [],
  };
}

/**
 * Map extent, tile URL template and place names for a bundled map
 * (e.g. "nasdara"). Rejects when that map is not installed — the map view then
 * falls back to a coordinate grid.
 */
export async function fetchMapMeta(mapId) {
  return requestJSON(`/api/maps/${encodeURIComponent(mapId)}/meta`);
}

/** @returns {Promise<Array<{id: string, name: string, size: number}>>} */
export async function fetchMaps() {
  const payload = await requestJSON('/api/maps');
  return payload?.maps ?? [];
}

/**
 * The instance's live RCON ban list. The Players tab uses it to offer "Unban"
 * only for players whose SteamID is actually banned.
 * @returns {Promise<Array<{id: number, guid?: string, ip?: string, minutes: number, reason?: string}>>}
 */
export async function fetchBans(instance) {
  const payload = await requestJSON(`/api/instances/${encodeURIComponent(instance)}/bans`);
  return payload?.bans ?? [];
}

/**
 * Remember which player a ban belongs to (bans only carry a BattlEye GUID).
 * @returns {Promise<{guid: string, player: string}>}
 */
export async function linkBan(instance, guid, player) {
  return requestJSON(`/api/instances/${encodeURIComponent(instance)}/bans/link`, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ guid, player }),
  });
}

/**
 * The Steam admin list plus any refused sign-ins waiting for approval.
 * @returns {Promise<{steam: boolean, writable: boolean, admins: Array<{steamID: string, name?: string}>, pending: Array<{steamID: string, firstAt: string, lastAt: string, attempts: number}>}>}
 */
export async function fetchAdmins() {
  return requestJSON('/api/admins');
}

/**
 * Grant or revoke Steam admin rights. The server writes web.auth.steam.admins
 * in config.yaml and applies the change immediately.
 * @param {'add'|'remove'} action
 * @returns {Promise<object>} the updated admin list
 */
export async function updateAdmin(steamID, name, action) {
  return requestJSON('/api/admins', {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ steamID, name: name || '', action }),
  });
}

/**
 * Run one RCON action for a player from the analysed log. The server resolves
 * the live session id (kick/ban) or the ban-list entry (unban) itself.
 * @param {'kick'|'ban'|'unban'} action
 * @param {{name: string, ids?: string[], minutes?: number, reason?: string}} payload
 * @returns {Promise<{action: string, player: string, message: string}>}
 */
export async function playerAction(instance, action, payload) {
  return requestJSON(`/api/instances/${encodeURIComponent(instance)}/players/${encodeURIComponent(action)}`, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify(payload),
  });
}

/**
 * Analyse a selection (or everything when `files` is empty).
 * @returns {Promise<object>} the report the views expect
 */
export async function analyze(instance, files) {
  const body = files && files.length ? { files } : {};
  const report = await requestJSON(`/api/instances/${encodeURIComponent(instance)}/analyze`, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify(body),
  });
  return normalizeReport(report);
}

/** Adds the lookup structures the views rely on. */
function normalizeReport(report) {
  if (report?.adm?.players) {
    report.adm.playerByKey = new Map(report.adm.players.map((p) => [p.key, p]));
  }
  return report;
}
