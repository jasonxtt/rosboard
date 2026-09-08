import assert from 'node:assert/strict'
import test from 'node:test'
import { terminalNameDraft, terminalNamePlaceholder, terminalNameSubmission } from '../src/compact/lib/terminalName.ts'

const automaticTerminal = { autoName: 'iPhone', customName: '', displayName: 'iPhone' }

test('automatic names stay out of the custom-name draft', () => {
  assert.equal(terminalNameDraft(automaticTerminal), '')
  assert.equal(terminalNamePlaceholder(automaticTerminal), 'iPhone')
})

test('confirming an unchanged name does not submit metadata', () => {
  assert.equal(terminalNameSubmission('', ''), null)
  assert.equal(terminalNameSubmission('  ', ''), null)
  assert.equal(terminalNameSubmission('My Phone', 'My Phone'), null)
  assert.equal(terminalNameSubmission(' My Phone ', 'My Phone'), null)
})

test('custom names persist and clearing one restores automatic mode', () => {
  assert.equal(terminalNameSubmission('My Phone', ''), 'My Phone')
  assert.equal(terminalNameSubmission('', 'My Phone'), '')
})

test('the automatic placeholder follows the current automatic name', () => {
  assert.equal(terminalNamePlaceholder({ ...automaticTerminal, autoName: 'Jason-iPhone', displayName: 'Jason-iPhone' }), 'Jason-iPhone')
  assert.equal(terminalNamePlaceholder({ autoName: '', customName: '', displayName: '10.0.0.8' }), '10.0.0.8')
})
