import * as oxcParser from 'eslint-parser-oxc'
import jsxA11y from 'eslint-plugin-jsx-a11y'
import reactHooks from 'eslint-plugin-react-hooks'

// gui builds with TypeScript 7, which ships no JavaScript API, so
// typescript-eslint cannot parse it; the Oxc-backed parser needs no
// `typescript`. The build type-checks the code, so ESLint here covers only
// what the type checker cannot see: accessibility in the shared components and
// the Rules of Hooks.
export default [
  { ignores: ['dist'] },
  {
    files: ['**/*.{ts,tsx}'],
    languageOptions: { parser: oxcParser },
  },
  jsxA11y.flatConfigs.recommended,
  {
    plugins: { 'react-hooks': reactHooks },
    rules: {
      'react-hooks/rules-of-hooks': 'error',
      'react-hooks/exhaustive-deps': 'error',
    },
  },
]
