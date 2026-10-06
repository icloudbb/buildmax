import js from '@eslint/js'
import globals from 'globals'
import tseslint from 'typescript-eslint'
import reactHooks from 'eslint-plugin-react-hooks'
import jsxA11y from 'eslint-plugin-jsx-a11y'
import reactRefresh from 'eslint-plugin-react-refresh'

// `npm run build` already type-checks with `tsc -b`; ESLint covers what the
// type checker cannot see — hook rules, unreachable code, unused bindings.
export default tseslint.config(
  { ignores: ['dist'] },
  js.configs.recommended,
  ...tseslint.configs.recommended,
  reactHooks.configs.flat['recommended-latest'],
  jsxA11y.flatConfigs.recommended,
  {
    plugins: { 'react-refresh': reactRefresh },
    rules: {
      // Context providers intentionally export their hooks beside the provider.
      // This only reduces Fast Refresh granularity; it is not a correctness
      // issue and splitting those public modules would make navigation worse.
      'react-refresh/only-export-components': 'off',
    },
  },
  {
    // Static assets copied into the bundle verbatim: classic browser scripts,
    // not modules, and not type-checked by `tsc -b`.
    files: ['public/**/*.js'],
    languageOptions: {
      ecmaVersion: 2022,
      globals: globals.browser,
      sourceType: 'script',
    },
  },
  {
    // Build-time node scripts run by the Docker image build, not the browser.
    files: ['scripts/**/*.mjs'],
    languageOptions: {
      ecmaVersion: 2022,
      globals: globals.node,
    },
  },
  {
    files: ['**/*.{ts,tsx}'],
    languageOptions: {
      ecmaVersion: 2022,
      globals: globals.browser,
    },
    rules: {
      // An argument prefixed with _ is deliberately unused, usually to keep a
      // callback's shape.
      '@typescript-eslint/no-unused-vars': ['error', { argsIgnorePattern: '^_' }],
      // These React Compiler eligibility rules flag established non-compiler
      // patterns (async loading effects and composed hook return objects). Keep
      // the correctness-oriented Rules of Hooks and exhaustive-deps enabled.
      'react-hooks/refs': 'off',
      'react-hooks/set-state-in-effect': 'off',
      // WebKit drops list semantics from a list styled `list-style: none`, so
      // an explicit role="list" on one is deliberate. `nav` keeps the default.
      'jsx-a11y/no-redundant-roles': ['error', { nav: ['navigation'], ul: ['list'], ol: ['list'] }],
    },
  },
)
