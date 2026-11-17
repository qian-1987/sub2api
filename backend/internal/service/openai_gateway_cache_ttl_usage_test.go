//go:build unit

package service

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/apicompat"
	"github.com/stretchr/testify/require"
)

const openAIMixedCacheTTLUsage = `{"input_tokens":12000,"output_tokens":10,"input_tokens_details":{"cached_tokens":4000,"cache_write_tokens":7000,"cache_write_tokens_details":{"ttl_30m_tokens":3000,"ttl_12h_tokens":4000}}}`

func TestOpenAICacheTTLUsageHTTPAndBridge(t *testing.T) {
	usage, ok := extractOpenAIUsageFromJSONBytes([]byte(`{"usage":` + openAIMixedCacheTTLUsage + `}`))
	require.True(t, ok)
	require.Equal(t, 12000, usage.InputTokens)
	require.Equal(t, 7000, usage.CacheCreationInputTokens)
	require.Equal(t, 3000, usage.CacheCreation30mInputTokens)
	require.Equal(t, 4000, usage.CacheCreation12hInputTokens)
	require.True(t, usage.CacheCreationBreakdownPresent)
	require.Equal(t, 1000, usage.InputTokens-usage.CacheReadInputTokens-usage.CacheCreationInputTokens)

	var bridged apicompat.ResponsesUsage
	require.NoError(t, json.Unmarshal([]byte(openAIMixedCacheTTLUsage), &bridged))
	require.Equal(t, usage, copyOpenAIUsageFromResponsesUsage(&bridged))
}

func TestOpenAICacheTTLUsageStreamSnapshotsDoNotAccumulate(t *testing.T) {
	for _, transport := range []string{"sse", "ws"} {
		t.Run(transport, func(t *testing.T) {
			var usage OpenAIUsage
			parse := func(body string) {
				if transport == "sse" {
					(&OpenAIGatewayService{}).parseSSEUsage(body, &usage)
				} else {
					parseOpenAIWSResponseUsageFromCompletedEvent([]byte(body), &usage)
				}
			}
			parse(`{"type":"response.in_progress","response":{"usage":` + openAIMixedCacheTTLUsage + `}}`)
			terminal := `{"type":"response.completed","response":{"usage":` + openAIMixedCacheTTLUsage + `}}`
			parse(terminal)
			parse(terminal)
			require.Equal(t, 7000, usage.CacheCreationInputTokens)
			require.Equal(t, 3000, usage.CacheCreation30mInputTokens)
			require.Equal(t, 4000, usage.CacheCreation12hInputTokens)
			parse(`{"type":"response.completed","response":{"usage":{"input_tokens":12000,"output_tokens":10,"input_tokens_details":{"cache_write_tokens":7000,"cache_write_tokens_details":{"ttl_30m_tokens":7000,"ttl_12h_tokens":0}}}}}`)
			require.Equal(t, 7000, usage.CacheCreation30mInputTokens)
			require.Zero(t, usage.CacheCreation12hInputTokens)
		})
	}
}

func TestOpenAICacheTTLUsageProgressiveAggregateClearsStaleBuckets(t *testing.T) {
	var usage OpenAIUsage
	svc := &OpenAIGatewayService{}
	svc.parseSSEUsage(`{"type":"response.in_progress","response":{"usage":`+openAIMixedCacheTTLUsage+`}}`, &usage)
	svc.parseSSEUsage(`{"type":"response.in_progress","usage":{"input_tokens_details":{"cache_write_tokens":8000}}}`, &usage)
	require.Equal(t, 8000, usage.CacheCreationInputTokens)
	require.Zero(t, usage.CacheCreation30mInputTokens)
	require.Zero(t, usage.CacheCreation12hInputTokens)
	require.False(t, usage.CacheCreationBreakdownPresent)
	svc.parseSSEUsage(`{"type":"response.in_progress","usage":{"input_tokens_details":{"cache_write_tokens":0,"cache_write_tokens_details":{"ttl_30m_tokens":0,"ttl_12h_tokens":0}}}}`, &usage)
	require.Zero(t, usage.CacheCreationInputTokens)
	require.True(t, usage.CacheCreationBreakdownPresent)
}

func TestOpenAICacheTTLUsageRecordUsagePersistsAndBillsDisjointBuckets(t *testing.T) {
	usageRepo := &openAIRecordUsageLogRepoStub{inserted: true}
	svc := newOpenAIRecordUsageServiceForTest(usageRepo, &openAIRecordUsageUserRepoStub{}, &openAIRecordUsageSubRepoStub{}, nil)
	svc.billingService = NewBillingService(svc.cfg, &PricingService{pricingData: map[string]*LiteLLMModelPricing{
		"gpt-6.1-terra": {InputCostPerToken: 1e-6, OutputCostPerToken: 5e-6, CacheReadInputTokenCost: .05e-6},
	}})
	usage, ok := extractOpenAIUsageFromJSONBytes([]byte(`{"usage":` + openAIMixedCacheTTLUsage + `}`))
	require.True(t, ok)
	err := svc.RecordUsage(context.Background(), &OpenAIRecordUsageInput{
		Result: &OpenAIForwardResult{RequestID: "resp_openai_cache_ttls", Usage: usage, Model: "gpt-6.1-terra", Duration: time.Second},
		APIKey: &APIKey{ID: 1056}, User: &User{ID: 2056}, Account: &Account{ID: 3056, Platform: PlatformOpenAI},
	})
	require.NoError(t, err)
	log := usageRepo.lastLog
	require.NotNil(t, log)
	require.Equal(t, 1000, log.InputTokens)
	require.Equal(t, 7000, log.CacheCreationTokens)
	require.Equal(t, 3000, log.CacheCreation5mTokens) // OpenAI compatibility slot: 30m.
	require.Equal(t, 4000, log.CacheCreation1hTokens) // OpenAI compatibility slot: 12h.
	require.Equal(t, 12010, log.TotalTokens())
	require.InDelta(t, .001, log.InputCost, 1e-12)
	require.InDelta(t, .01375, log.CacheCreationCost, 1e-12)
	require.InDelta(t, .0002, log.CacheReadCost, 1e-12)
	require.InDelta(t, log.TotalCost*1.1, log.ActualCost, 1e-12)
}

func TestOpenAICacheTTLUsageExplicitZeroClearsProgressiveWrites(t *testing.T) {
	for _, transport := range []string{"sse", "ws"} {
		for _, event := range []string{"response.in_progress", "response.completed"} {
			t.Run(transport+"/"+event, func(t *testing.T) {
				var usage OpenAIUsage
				parse := func(body string) {
					if transport == "sse" {
						(&OpenAIGatewayService{}).parseSSEUsage(body, &usage)
					} else {
						parseOpenAIWSResponseUsageFromCompletedEvent([]byte(body), &usage)
					}
				}
				parse(`{"type":"response.in_progress","response":{"usage":` + openAIMixedCacheTTLUsage + `}}`)
				// A missing aggregate must keep the earlier write usage.
				parse(`{"type":"` + event + `","response":{"usage":{"input_tokens":0,"output_tokens":0}}}`)
				require.Equal(t, 7000, usage.CacheCreationInputTokens)
				parse(`{"type":"` + event + `","response":{"usage":{"input_tokens":0,"output_tokens":0,"input_tokens_details":{"cache_write_tokens":0}}}}`)
				require.Zero(t, usage.CacheCreationInputTokens)
				require.Zero(t, usage.CacheCreation30mInputTokens)
				require.Zero(t, usage.CacheCreation12hInputTokens)
				require.False(t, usage.CacheCreationBreakdownPresent)
				require.Equal(t, 12000, usage.InputTokens)
				require.Equal(t, 10, usage.OutputTokens)
			})
		}
	}
}
