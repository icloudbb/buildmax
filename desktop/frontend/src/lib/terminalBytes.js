// Terminal output crosses the Wails bridge base64-encoded, because a PTY's bytes
// are not guaranteed valid UTF-8 and JSON would mangle raw control sequences.
// Decode back to bytes before handing them to the emulator.
export function decodeBase64ToBytes(b64) {
  const binary = atob(b64);
  const bytes = new Uint8Array(binary.length);
  for (let i = 0; i < binary.length; i += 1) {
    bytes[i] = binary.charCodeAt(i);
  }
  return bytes;
}
