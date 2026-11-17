package service

import (
	"testing"

	openaiwsv2 "github.com/Wei-Shaw/sub2api/internal/service/openai_ws_v2"
)

func TestQikeWSV2UsageConversionPreservesBuckets(t *testing.T) {
	got := openAIWSV2UsageFromRelay(openaiwsv2.Usage{
		InputTokens: 12, ImageInputTokens: 2, OutputTokens: 4,
		CacheCreationInputTokens: 7, CacheCreation30mInputTokens: 3,
		CacheCreation12hInputTokens: 4, CacheReadInputTokens: 5, ImageOutputTokens: 6,
	})
	want := OpenAIUsage{
		InputTokens: 12, ImageInputTokens: 2, OutputTokens: 4,
		CacheCreationInputTokens: 7, CacheCreation30mInputTokens: 3,
		CacheCreation12hInputTokens: 4, CacheReadInputTokens: 5, ImageOutputTokens: 6,
	}
	if got != want {
		t.Fatalf("relay usage lost billing fields: got %+v, want %+v", got, want)
	}
}
