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
