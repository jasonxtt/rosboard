import assert from 'node:assert/strict'
import test from 'node:test'
import React from 'react'
import { renderToStaticMarkup } from 'react-dom/server'
import { CopyButton } from '../src/ui/CopyButton.tsx'

Object.assign(globalThis, { React })

test('script copy button renders a Chinese text label without copy glyphs', () => {
  const html = renderToStaticMarkup(
    <CopyButton text="/system identity print" label="复制脚本" showText className="copy-script-button" />,
  )

  assert.match(html, /class="icon-btn copy-script-button"/)
  assert.match(html, />复制脚本<\/button>/)
  assert.doesNotMatch(html, /⧉|✓/)
})
