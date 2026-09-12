import assert from 'node:assert/strict'
import test from 'node:test'
import { DEFAULT_INCLUDE_KEYWORD_DOMAINS, initialIncludeKeywordDomains } from '../src/features/policy/canonical.ts'

test('new RoutingRule defaults keyword projection off while edits preserve the saved value', () => {
 assert.equal(DEFAULT_INCLUDE_KEYWORD_DOMAINS, false)
 assert.equal(initialIncludeKeywordDomains(null), false)
 assert.equal(initialIncludeKeywordDomains({ includeKeywordDomains: false }), false)
 assert.equal(initialIncludeKeywordDomains({ includeKeywordDomains: true }), true)
})
