import path from 'node:path';
import { fileURLToPath } from 'node:url';
import { defineConfig } from 'vite';
import react from '@vitejs/plugin-react';

const __dirname = path.dirname(fileURLToPath(import.meta.url));

export default defineConfig({
  plugins: [react()],
  root: '.',
  // Component tests need a document. The pure-helper tests do not, but one
  // environment for the whole suite beats per-file annotations.
  test: {
    environment: 'jsdom',
    // e2e/ holds Playwright specs, which need a browser and a running `wails
    // dev`. Vitest would otherwise collect them by extension and fail on the
    // import. `./make e2e desktop-ui` runs those; the visual/ screenshot specs
    // are `./make e2e visual`.
    exclude: ['node_modules/**', 'dist/**', 'e2e/**', 'visual/**'],
  },
  resolve: {
    // @buildmax/gui is a symlinked workspace package that externalises react,
    // so its bare `import "react"` resolves from its own real path — and gui
    // has react installed as a peer. Without this, the bundle ships two React
    // instances and every hook throws "Cannot read properties of null", which
    // the app shows as a blank window. Portal dedupes for the same reason.
    dedupe: ['react', 'react-dom', 'react/jsx-runtime'],
    alias: {
      // Resolve CSS subpath so Vite does not pass the bare specifier to the file system (avoids ENOENT)
      '@buildmax/gui/theme.css': path.resolve(__dirname, 'node_modules/@buildmax/gui/dist/theme.css'),
    },
  },
  build: {
    // Built one level up, into desktop/dist, rather than inside this directory.
    // desktop/assets_embed.go embeds it, and //go:embed cannot cross a module
    // boundary — this directory is its own Go module so that the node_modules
    // it installs stays out of the root module. See ../../portal/go.mod.
    outDir: '../dist',
    emptyOutDir: true,
  },
});
