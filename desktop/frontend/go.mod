// The desktop frontend contains no Go code — the Wails Go side is one
// directory up, in package desktop, which stays in the root module. This
// module is a boundary that keeps desktop/frontend/node_modules out of the
// root module's `./...`; see portal/go.mod for the full rationale.
//
// Because //go:embed cannot cross a module boundary, the Vite build writes to
// desktop/dist rather than desktop/frontend/dist, so that desktop/assets_embed.go
// embeds a directory inside its own module. See vite.config.js.
module desktopfrontend

go 1.26.9
