import stylelint from 'stylelint'
import { tokenConfig } from './stylelint/tokens.mjs'

export default tokenConfig(stylelint, import.meta.dirname)
