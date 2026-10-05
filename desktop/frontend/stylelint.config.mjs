import stylelint from 'stylelint'
import { tokenConfig } from '../../gui/stylelint/tokens.mjs'

export default tokenConfig(stylelint, import.meta.dirname)
