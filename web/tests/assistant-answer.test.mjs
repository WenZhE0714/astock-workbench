import assert from 'node:assert/strict'
import { renderAssistantAnswer } from '../src/assistant-answer.mjs'

const rendered = renderAssistantAnswer('## 当前判断\n\n**短线偏弱**，观察 `MA20`。\n\n- 第一项证据\n- 第二项证据\n\n> 数据时效：10:00')
assert.match(rendered, /<h2>当前判断<\/h2>/)
assert.match(rendered, /<strong>短线偏弱<\/strong>/)
assert.match(rendered, /<code>MA20<\/code>/)
assert.match(rendered, /<ul>/)
assert.match(rendered, /<blockquote>/)
for (const payload of [
  '<script>alert(1)</script>',
  '<img src=x onerror=alert(1)>',
  '[unsafe](javascript:alert(1))',
  '[unsafe](data:text/html;base64,PHNjcmlwdD4=)',
  '[unsafe](jav&#x61;script:alert(1))',
  '![remote](https://example.com/track.png)',
]) {
  const html = renderAssistantAnswer(payload)
  assert.doesNotMatch(html, /<(script|img)\b|href="(?:javascript|data):/i)
}
assert.equal(renderAssistantAnswer(null), '')
console.log('Assistant Markdown rendering and untrusted-content checks passed')
