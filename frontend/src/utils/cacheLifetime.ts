/** The API retains its 5m/1h field names for both providers. */
export function usesOpenAICacheLifetimes(model: string, platform?: string): boolean {
  if (platform && platform !== 'openai') return false
  const normalized = (model.split('/').pop() ?? '').trim().toLowerCase().replace(/[_\s]+/g, '-').replace(/-+/g, '-').replace(/^gpt5/, 'gpt-5')
  if (normalized === 'gpt-6' || normalized === 'gpt-5.6') return true
  if (/^gpt-5\.6-(?:max|none|minimal|low|medium|high|xhigh|\d{4}-\d{2}-\d{2})$/.test(normalized)) return true
  return /^(?:gpt-5\.6-(?:sol|terra|luna)|gpt-6-(?:astra|sol|luna)|gpt-6\.1-(?:sol|terra))(?:-|$)/.test(normalized)
}

export function cacheWriteDurations(model: string, platform?: string) {
  return usesOpenAICacheLifetimes(model, platform)
    ? { short: '30m', long: '12h' }
    : { short: '5m', long: '1h' }
}
