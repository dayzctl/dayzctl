/** Severity vocabulary shared by the rule engine and the views. */

export const SEVERITY = {
  CRITICAL: 'critical',
  ERROR: 'error',
  WARNING: 'warning',
  INFO: 'info',
};

export const SEVERITY_ORDER = [SEVERITY.CRITICAL, SEVERITY.ERROR, SEVERITY.WARNING, SEVERITY.INFO];

export const SEVERITY_LABEL = {
  [SEVERITY.CRITICAL]: 'Critical',
  [SEVERITY.ERROR]: 'Error',
  [SEVERITY.WARNING]: 'Warning',
  [SEVERITY.INFO]: 'Info',
};

export function severityRank(severity) {
  const idx = SEVERITY_ORDER.indexOf(severity);
  return idx === -1 ? SEVERITY_ORDER.length : idx;
}
