import assert from 'node:assert/strict'
import { readFileSync } from 'node:fs'
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

function readSource(relativePath: string) {
  return readFileSync(new URL(`../src/${relativePath}`, import.meta.url), 'utf8')
}

test('all Aurora generated-script copy callsites use the Chinese text button', () => {
  const auroraCallsites = [
    'pages/RouterOSSetupPage.tsx',
    'features/settings/QuickOnboardingWizard.tsx',
    'features/settings/AccountCard.tsx',
    'features/settings/CleanupCard.tsx',
  ]

  for (const relativePath of auroraCallsites) {
    const source = readSource(relativePath)
    const matches = source.match(/<CopyButton\b[^>]*label="复制脚本"[^>]*showText[^>]*className="copy-script-button"[^>]*\/>/g) ?? []
    assert.equal(matches.length, 1, `${relativePath} must have one text-labelled script copy callsite`)
    assert.doesNotMatch(matches[0], /⧉|✓/)
  }
})

test('all Compact generated-script copy callsites use Chinese text states', () => {
  const source = readSource('compact/App.tsx')
  const matches = source.match(/<button\b[^>]*className="toolbar-button copy-script-button"[^>]*>[\s\S]*?<\/button>/g) ?? []

  assert.equal(matches.length, 3, 'Compact must keep cleanup, onboarding, and replacement script copy callsites')
  for (const match of matches) {
    assert.match(match, /已复制/)
    assert.match(match, /复制脚本/)
    assert.doesNotMatch(match, /⧉|✓/)
  }
})

test('Aurora script copy buttons keep the mobile touch target aligned with nearby controls', () => {
  const source = readFileSync(new URL('../src/styles/base.css', import.meta.url), 'utf8')
  assert.match(source, /@media \(max-width: 767px\)[\s\S]*?\.copy-script-button\s*\{\s*min-height: 40px;\s*height: 40px;/)
})
