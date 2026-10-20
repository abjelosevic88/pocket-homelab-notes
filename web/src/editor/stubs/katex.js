// Formulas are switched off in this editor, so the typesetter is not shipped.
const katex = { render() {}, renderToString() { return '' } }
export default katex
export const render = katex.render
export const renderToString = katex.renderToString
