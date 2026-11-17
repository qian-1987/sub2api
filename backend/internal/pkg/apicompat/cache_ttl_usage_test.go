package apicompat

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestResponsesCacheTTLUsageSurvivesChatBridge(t *testing.T) {
	var usage ResponsesUsage
	require.NoError(t, json.Unmarshal([]byte(`{"input_tokens":12000,"output_tokens":10,"input_tokens_details":{"cached_tokens":4000,"cache_write_tokens":7000,"cache_write_tokens_details":{"ttl_30m_tokens":3000,"ttl_12h_tokens":4000}}}`), &usage))
	require.Equal(t, 7000, usage.CacheCreationInputTokens)
	short, long, ok := usage.InputTokensDetails.CacheWriteTokensDetails.Counts(usage.CacheCreationInputTokens)
	require.True(t, ok)
	require.Equal(t, 3000, short)
	require.Equal(t, 4000, long)
	chat := chatUsageFromResponsesUsage(&usage)
	require.NotNil(t, chat.PromptTokensDetails.CacheWriteTokensDetails)
	roundTrip := ChatUsageToResponsesUsage(chat)
	require.Equal(t, usage, *roundTrip)

	encoded, err := json.Marshal(chat)
	require.NoError(t, err)
	require.Contains(t, string(encoded), `"ttl_30m_tokens":3000`)
	require.Contains(t, string(encoded), `"ttl_12h_tokens":4000`)
}

func TestResponsesCacheTTLUsageZeroAndMissingAggregate(t *testing.T) {
	for _, tt := range []struct {
		name, body string
		total      int
		ok         bool
	}{
		{"missing total", `{"input_tokens_details":{"cache_write_tokens_details":{"ttl_30m_tokens":3,"ttl_12h_tokens":4}}}`, 7, true},
		{"explicit zero", `{"input_tokens_details":{"cache_write_tokens":0,"cache_write_tokens_details":{"ttl_30m_tokens":0,"ttl_12h_tokens":0}}}`, 0, true},
		{"zero beats contradictory details", `{"input_tokens_details":{"cache_write_tokens":0,"cache_write_tokens_details":{"ttl_30m_tokens":3,"ttl_12h_tokens":4}}}`, 0, false},
		{"partial details", `{"input_tokens_details":{"cache_write_tokens":7,"cache_write_tokens_details":{"ttl_12h_tokens":7}}}`, 7, false},
		{"no invented details", `{"input_tokens_details":{"cache_write_tokens":7}}`, 7, false},
	} {
		t.Run(tt.name, func(t *testing.T) {
			var usage ResponsesUsage
			require.NoError(t, json.Unmarshal([]byte(tt.body), &usage))
			require.Equal(t, tt.total, usage.CacheCreationInputTokens)
			_, _, ok := usage.InputTokensDetails.CacheWriteTokensDetails.Counts(usage.CacheCreationInputTokens)
			require.Equal(t, tt.ok, ok)
		})
	}
}

func TestResponsesCacheTTLUsageChatDetailsOnlyAndExplicitZero(t *testing.T) {
	for _, tt := range []struct {
		name, aggregate string
		want            int
	}{
		{"missing aggregate", "", 7},
		{"explicit canonical zero", `"cache_write_tokens":0,`, 0},
		{"explicit legacy zero", `"cache_creation_tokens":0,`, 0},
	} {
		t.Run(tt.name, func(t *testing.T) {
			var chat ChatUsage
			require.NoError(t, json.Unmarshal([]byte(`{"prompt_tokens":20,"completion_tokens":2,"prompt_tokens_details":{`+tt.aggregate+`"cache_write_tokens_details":{"ttl_30m_tokens":3,"ttl_12h_tokens":4}}}`), &chat))
			result := ChatUsageToResponsesUsage(&chat)
			require.Equal(t, tt.want, result.CacheCreationInputTokens)
			require.Equal(t, tt.want, result.InputTokensDetails.CacheWriteTokens)
			encoded, err := json.Marshal(result)
			require.NoError(t, err)
			var roundTrip ResponsesUsage
			require.NoError(t, json.Unmarshal(encoded, &roundTrip))
			require.Equal(t, tt.want, roundTrip.CacheCreationInputTokens)
		})
	}
}

func TestResponsesCacheTTLUsageReverseRoundTripRejectsContradictoryDetails(t *testing.T) {
	for _, tt := range []struct {
		name, details string
		want          int
	}{
		{"explicit zero", `"cache_write_tokens":0,"cache_write_tokens_details":{"ttl_30m_tokens":3,"ttl_12h_tokens":4}`, 0},
		{"mismatched positive", `"cache_write_tokens":5,"cache_write_tokens_details":{"ttl_30m_tokens":3,"ttl_12h_tokens":4}`, 5},
		{"partial", `"cache_write_tokens":5,"cache_write_tokens_details":{"ttl_12h_tokens":5}`, 5},
	} {
		t.Run(tt.name, func(t *testing.T) {
			var original ResponsesUsage
			require.NoError(t, json.Unmarshal([]byte(`{"input_tokens":20,"output_tokens":2,"input_tokens_details":{`+tt.details+`}}`), &original))
			chat := chatUsageFromResponsesUsage(&original)
			require.Nil(t, chat.PromptTokensDetails.CacheWriteTokensDetails)
			require.Equal(t, tt.want, ChatUsageToResponsesUsage(chat).CacheCreationInputTokens)
			encoded, err := json.Marshal(chat)
			require.NoError(t, err)
			var decoded ChatUsage
			require.NoError(t, json.Unmarshal(encoded, &decoded))
			roundTrip := ChatUsageToResponsesUsage(&decoded)
			require.Equal(t, tt.want, roundTrip.CacheCreationInputTokens)
			encoded, err = json.Marshal(roundTrip)
			require.NoError(t, err)
			var finalUsage ResponsesUsage
			require.NoError(t, json.Unmarshal(encoded, &finalUsage))
			require.Equal(t, tt.want, finalUsage.CacheCreationInputTokens)
		})
	}
}
