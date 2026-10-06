// Shared Stylelint configuration for gui, portal, and desktop/frontend: colors
// come from the tokens in gui/src/theme.css, and every var() names a custom
// property some stylesheet defines. An undefined property with no fallback makes
// its whole declaration silently do nothing, which is how a set of undefined
// tokens once went unnoticed.
//
// Each package's stylelint.config.mjs passes in its own `stylelint` so the rule
// below registers with the instance that runs it; this file imports nothing
// from node_modules and therefore needs no install of its own.
import fs from 'node:fs'
import path from 'node:path'

const guiSrc = path.join(import.meta.dirname, '..', 'src')
const ruleName = 'buildmax/no-undefined-custom-property'

function cssFiles(dir) {
  return fs.readdirSync(dir, { recursive: true })
    .filter((name) => name.endsWith('.css'))
    .map((name) => path.join(dir, name))
}

// Definitions are declarations like `--name: value`. A fallback does not count
// as a definition: var(--x, red) still references a property nobody defines.
function definedProperties(files) {
  const defined = new Set()
  for (const file of files) {
    const css = fs.readFileSync(file, 'utf8').replace(/\/\*[\s\S]*?\*\//g, '')
    for (const m of css.matchAll(/(?:^|[{;\s])(--[\w-]+)\s*:/g)) defined.add(m[1])
  }
  return defined
}

// tokenConfig returns the Stylelint config for the package at packageDir. The
// known custom properties are everything gui's stylesheets define (Portal and
// Desktop import them) plus the package's own src/**/*.css.
export function tokenConfig(stylelint, packageDir) {
  const files = new Set([...cssFiles(guiSrc), ...cssFiles(path.join(packageDir, 'src'))])
  const defined = definedProperties(files)
  const messages = stylelint.utils.ruleMessages(ruleName, {
    rejected: (name) =>
      `Unexpected undefined custom property "${name}"; use a token from gui/src/theme.css or define it`,
  })
  const rule = (enabled) => (root, result) => {
    if (!enabled) return
    root.walkDecls((decl) => {
      for (const m of decl.value.matchAll(/var\(\s*(--[\w-]+)/g)) {
        if (defined.has(m[1])) continue
        stylelint.utils.report({ result, ruleName, node: decl, word: m[1], message: messages.rejected(m[1]) })
      }
    })
  }
  rule.ruleName = ruleName
  rule.messages = messages

  return {
    plugins: [stylelint.createPlugin(ruleName, rule)],
    rules: {
      [ruleName]: true,
      'color-no-hex': true,
      'color-named': 'never',
      'function-disallowed-list': ['rgb', 'rgba', 'hsl', 'hsla', 'hwb', 'lab', 'lch', 'oklab', 'oklch', 'color'],
    },
    overrides: [
      {
        // The one file allowed to hold color literals: it is where they become tokens.
        files: [path.relative(packageDir, path.join(guiSrc, 'theme.css'))],
        rules: { 'color-no-hex': null, 'color-named': null, 'function-disallowed-list': null },
      },
    ],
  }
}
