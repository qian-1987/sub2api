// Display names from the OpenAI API model catalog. Keep the model IDs as the form values.
const openAIModelDisplayNames = new Map([
  ['gpt-5.6', 'GPT-5.6 Sol'],
  ['gpt-5.6-luna', 'GPT-5.6 Luna'],
  ['gpt-5.6-sol', 'GPT-5.6 Sol'],
  ['gpt-5.6-terra', 'GPT-5.6 Terra'],
  ['gpt-6-astra', 'GPT-6 Astra'],
  ['gpt-6-luna', 'GPT-6 Luna'],
  ['gpt-6-sol', 'GPT-6 Sol'],
  ['gpt-6.1-sol', 'GPT-6.1 Sol'],
  ['gpt-6.1-terra', 'GPT-6.1 Terra'],
  ['gpt-6.2-astra', 'GPT-6.2 Astra'],
  ['gpt-6.2-sol', 'GPT-6.2 Sol'],
  ['gpt-image-2', 'GPT-Image-2'],
  ['gpt-image-2.5-flare', 'GPT-Image-2.5 Flare'],
  ['gpt-image-2.5-sunburst', 'GPT-Image-2.5 Sunburst'],
  ['gpt-image-2.6-flare', 'GPT-Image-2.6 Flare'],
  ['gpt-image-2.6-sunburst', 'GPT-Image-2.6 Sunburst']
])

export function channelModelDisplayName(model: string, platform?: string): string {
  return platform === 'openai' ? openAIModelDisplayNames.get(model.toLowerCase()) ?? model : model
}
