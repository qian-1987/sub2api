import { describe, expect, it } from 'vitest'
import { cacheWriteDurations, usesOpenAICacheLifetimes } from '../cacheLifetime'

describe('provider cache lifetimes', () => {
  it.each(['gpt-5.6', 'gpt-5.6-terra', 'gpt-5.6-high', 'gpt-6', 'gpt-6-astra', 'gpt-6-sol', 'gpt-6.1-terra', 'openai/gpt-6.1-sol', 'provider/GPT_6.1_TERRA'])('uses OpenAI TTLs for %s', model => {
    expect(cacheWriteDurations(model, 'openai')).toEqual({ short: '30m', long: '12h' })
    expect(usesOpenAICacheLifetimes(model)).toBe(true)
  })

  it.each(['claude-sonnet-4-6', 'gpt-5.5', 'gpt-6.10', 'gpt-6-unrecognized', 'gpt-5.6-mini', 'gpt-image-2'])('does not relabel unrelated model %s', model => {
    expect(usesOpenAICacheLifetimes(model, 'openai')).toBe(false)
  })

  it('preserves Anthropic durations even for a model alias', () => {
    expect(cacheWriteDurations('gpt-6.1-terra', 'anthropic')).toEqual({ short: '5m', long: '1h' })
  })
})
