// xterm color themes for the terminal tabs. Without an explicit palette xterm
// falls back to its own defaults, which sit oddly against the app's surfaces and
// wash out program colors (git status, ls, grep, build logs). These give the
// emulator a full 16-color ANSI palette plus foreground/cursor/selection tuned
// for legibility, and a light and a dark variant so the terminal tracks the app
// theme rather than staying dark in a light window. Colors follow the widely
// recognized VS Code terminal palettes so output looks the way users expect.

const dark = {
  background: '#1e1e1e',
  foreground: '#d4d4d4',
  cursor: '#d4d4d4',
  cursorAccent: '#1e1e1e',
  selectionBackground: '#264f78',
  black: '#000000',
  red: '#cd3131',
  green: '#0dbc79',
  yellow: '#e5e510',
  blue: '#2472c8',
  magenta: '#bc3fbc',
  cyan: '#11a8cd',
  white: '#e5e5e5',
  brightBlack: '#666666',
  brightRed: '#f14c4c',
  brightGreen: '#23d18b',
  brightYellow: '#f5f543',
  brightBlue: '#3b8eea',
  brightMagenta: '#d670d6',
  brightCyan: '#29b8db',
  brightWhite: '#ffffff',
};

const light = {
  background: '#ffffff',
  foreground: '#333333',
  cursor: '#333333',
  cursorAccent: '#ffffff',
  selectionBackground: '#add6ff',
  black: '#000000',
  red: '#cd3131',
  green: '#00bc00',
  yellow: '#949800',
  blue: '#0451a5',
  magenta: '#bc05bc',
  cyan: '#0598bc',
  white: '#555555',
  brightBlack: '#666666',
  brightRed: '#cd3131',
  brightGreen: '#14ce14',
  brightYellow: '#b5ba00',
  brightBlue: '#0451a5',
  brightMagenta: '#bc05bc',
  brightCyan: '#0598bc',
  brightWhite: '#a5a5a5',
};

export const TERMINAL_THEMES = { dark, light };

// terminalThemeFor maps the app theme name to a palette, defaulting to dark for
// anything but an explicit 'light'.
export function terminalThemeFor(theme) {
  return theme === 'light' ? light : dark;
}
