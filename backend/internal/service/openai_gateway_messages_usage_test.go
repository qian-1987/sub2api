//go:build unit

package service

import (
	"encoding/json"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/pkg/apicompat"
	"github.com/stretchr/testify/require"
)

func TestCopyOpenAIUsageFromResponsesUsageTrustsCanonicalCacheCreationValue(t *testing.T) {
	usage := &apicompat.ResponsesUsage{
		InputTokens:              20,
		OutputTokens:             2,
		CacheCreationInputTokens: 0,
		InputTokensDetails: &apicompat.ResponsesInputTokensDetails{
			CachedTokens:     3,
			CacheWriteTokens: 19,
		},
	}

	got := copyOpenAIUsageFromResponsesUsage(usage)

	require.Equal(t, 20, got.InputTokens)
	require.Equal(t, 3, got.CacheReadInputTokens)
	require.Zero(t, got.CacheCreationInputTokens)
}

func TestCopyOpenAIUsageFromResponsesUsageCacheTTLs(t *testing.T) {
	for _, tt := range []struct {
		name, details string
		short, long   int
	}{
		{"mixed", `{"ttl_30m_tokens":3,"ttl_12h_tokens":4}`, 3, 4},
		{"no details", `null`, 0, 0},
		{"partial", `{"ttl_12h_tokens":7}`, 0, 0},
		{"mismatched", `{"ttl_30m_tokens":4,"ttl_12h_tokens":4}`, 0, 0},
		{"malformed", `{"ttl_30m_tokens":"3","ttl_12h_tokens":4}`, 0, 0},
	} {
		t.Run(tt.name, func(t *testing.T) {
			var usage apicompat.ResponsesUsage
			require.NoError(t, json.Unmarshal([]byte(`{"input_tokens":20,"input_tokens_details":{"cached_tokens":2,"cache_write_tokens":7,"cache_write_tokens_details":`+tt.details+`}}`), &usage))
			got := copyOpenAIUsageFromResponsesUsage(&usage)
			require.Equal(t, 7, got.CacheCreationInputTokens)
			require.Equal(t, 2, got.CacheReadInputTokens)
			require.Equal(t, tt.short, got.CacheCreation30mInputTokens)
			require.Equal(t, tt.long, got.CacheCreation12hInputTokens)
		})
	}
}

func TestResponsesCacheTTLBridgeMatchesRawUsageParsing(t *testing.T) {
	for _, tt := range []struct {
		name, details string
	}{
		{"valid aggregate and buckets", `"cache_write_tokens":7,"cache_write_tokens_details":{"ttl_30m_tokens":3,"ttl_12h_tokens":4}`},
		{"missing aggregate", `"cache_write_tokens_details":{"ttl_30m_tokens":3,"ttl_12h_tokens":4}`},
		{"explicit zero and conflicting legacy", `"cache_write_tokens":0,"cache_creation_tokens":7,"cache_write_tokens_details":{"ttl_30m_tokens":3,"ttl_12h_tokens":4}`},
		{"legacy aggregate only", `"cache_creation_tokens":7`},
		{"mismatched buckets", `"cache_write_tokens":7,"cache_write_tokens_details":{"ttl_30m_tokens":4,"ttl_12h_tokens":4}`},
	} {
		t.Run(tt.name, func(t *testing.T) {
			body := `{"input_tokens":20,"output_tokens":2,"input_tokens_details":{"cached_tokens":2,` + tt.details + `}}`
			direct, ok := extractOpenAIUsageFromJSONBytes([]byte(`{"usage":` + body + `}`))
			require.True(t, ok)
			var decoded apicompat.ResponsesUsage
			require.NoError(t, json.Unmarshal([]byte(body), &decoded))
			require.Equal(t, direct, copyOpenAIUsageFromResponsesUsage(&decoded))
		})
	}
}

func TestChatCacheAggregatePresenceSurvivesResponsesStreamParsing(t *testing.T) {
	for _, tt := range []struct {
		name, details string
		wantCache     int
	}{
		{"canonical zero", `{"cache_write_tokens":0}`, 0},
		{"legacy zero", `{"cache_creation_tokens":0}`, 0},
		{"canonical zero overrides legacy", `{"cache_write_tokens":0,"cache_creation_tokens":7}`, 0},
		{"missing aggregate", `{}`, 7},
		{"missing aggregate with buckets", `{"cache_write_tokens_details":{"ttl_30m_tokens":3,"ttl_12h_tokens":4}}`, 7},
	} {
		t.Run(tt.name, func(t *testing.T) {
			var chat apicompat.ChatUsage
			require.NoError(t, json.Unmarshal([]byte(`{"prompt_tokens":0,"completion_tokens":0,"prompt_tokens_details":`+tt.details+`}`), &chat))
			converted := apicompat.ChatUsageToResponsesUsage(&chat)
			encoded, err := json.Marshal(converted)
			require.NoError(t, err)
			usage := OpenAIUsage{InputTokens: 20, CacheCreationInputTokens: 7, CacheCreation30mInputTokens: 3, CacheCreation12hInputTokens: 4}
			(&OpenAIGatewayService{}).parseSSEUsage(`{"type":"response.completed","response":{"usage":`+string(encoded)+`}}`, &usage)
			require.Equal(t, tt.wantCache, usage.CacheCreationInputTokens)
			if tt.wantCache == 0 {
				require.Zero(t, usage.CacheCreation30mInputTokens)
				require.Zero(t, usage.CacheCreation12hInputTokens)
			} else {
				require.Equal(t, 3, usage.CacheCreation30mInputTokens)
				require.Equal(t, 4, usage.CacheCreation12hInputTokens)
			}
		})
	}
}

func TestResponsesCacheAggregateAliasesRoundTripMatchesStreamParsing(t *testing.T) {
	for _, tt := range []struct {
		name, fields string
	}{
		{"creation input zero", `"cache_creation_input_tokens":0`},
		{"write input zero", `"cache_write_input_tokens":0`},
		{"write zero", `"cache_write_tokens":0`},
		{"creation zero", `"cache_creation_tokens":0`},
		{"null", `"cache_write_tokens":null`},
		{"missing", `"total_tokens":0`},
		{"nested null blocks lower zero", `"input_tokens_details":{"cache_write_tokens":null},"prompt_tokens_details":{"cache_write_tokens":0}`},
		{"nested null blocks lower positive", `"input_tokens_details":{"cache_write_tokens":null},"prompt_tokens_details":{"cache_write_tokens":7}`},
		{"nested invalid blocks lower zero", `"input_tokens_details":{"cache_write_tokens":"invalid"},"prompt_tokens_details":{"cache_write_tokens":0}`},
		{"top null blocks lower zero", `"cache_write_tokens":null,"cache_creation_tokens":0`},
		{"higher positive beats lower zero", `"cache_write_tokens":7,"cache_creation_input_tokens":0`},
		{"lower positive prevents zero", `"cache_write_tokens":0,"cache_write_input_tokens":7`},
		{"nested positive beats top zero", `"input_tokens_details":{"cache_write_tokens":7},"cache_creation_input_tokens":0`},
	} {
		t.Run(tt.name, func(t *testing.T) {
			body := `{"input_tokens":0,"output_tokens":0,` + tt.fields + `}`
			var original apicompat.ResponsesUsage
			require.NoError(t, json.Unmarshal([]byte(body), &original))
			response := &apicompat.ResponsesResponse{ID: "resp_test", Usage: &original}
			chat := apicompat.ResponsesToChatCompletions(response, "test-model")
			encodedChat, err := json.Marshal(chat.Usage)
			require.NoError(t, err)
			var decodedChat apicompat.ChatUsage
			require.NoError(t, json.Unmarshal(encodedChat, &decodedChat))
			encodedResponses, err := json.Marshal(apicompat.ChatUsageToResponsesUsage(&decodedChat))
			require.NoError(t, err)
			direct := OpenAIUsage{InputTokens: 20, CacheCreationInputTokens: 7, CacheCreation30mInputTokens: 3, CacheCreation12hInputTokens: 4}
			bridged := direct
			service := &OpenAIGatewayService{}
			service.parseSSEUsage(`{"type":"response.completed","response":{"usage":`+body+`}}`, &direct)
			service.parseSSEUsage(`{"type":"response.completed","response":{"usage":`+string(encodedResponses)+`}}`, &bridged)
			require.Equal(t, direct, bridged)
		})
	}
}
