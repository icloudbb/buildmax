// Layout preferences are per machine, remembered across runs. Storage can be
// unavailable (private windows, cleared data), so every access is guarded and
// falls back to the default.

export function readStored(key, fallback) {
  try {
    const v = localStorage.getItem(key);
    return v == null ? fallback : JSON.parse(v);
  } catch {
    return fallback;
  }
}

export function writeStored(key, value) {
  try {
    localStorage.setItem(key, JSON.stringify(value));
  } catch {
    /* storage may be unavailable; the preference just does not persist */
  }
}
