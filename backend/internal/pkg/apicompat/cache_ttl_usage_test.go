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
		{"missing total", `{"input_tokens_details":{"cache_write_tokens_details":{"ttl_30m_tokens":3,"ttl_12h_tokens":4}}}`, 0, false},
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
		{"missing aggregate", "", 0},
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

func TestResponsesCacheTTLUsageExplicitZeroSuppressesLegacyAcrossJSON(t *testing.T) {
	for _, tt := range []struct {
		name, detail string
	}{
		{"positive buckets", `,"cache_write_tokens_details":{"ttl_30m_tokens":3000,"ttl_12h_tokens":4000}`},
		{"missing buckets", ``},
		{"null buckets", `,"cache_write_tokens_details":null`},
		{"explicit zero buckets", `,"cache_write_tokens_details":{"ttl_30m_tokens":0,"ttl_12h_tokens":0}`},
	} {
		t.Run(tt.name, func(t *testing.T) {
			var original ResponsesUsage
			body := `{"input_tokens":12000,"output_tokens":10,"input_tokens_details":{"cached_tokens":2000,"cache_write_tokens":0,"cache_creation_tokens":7000` + tt.detail + `}}`
			require.NoError(t, json.Unmarshal([]byte(body), &original))
			require.Zero(t, original.CacheCreationInputTokens)

			chat := chatUsageFromResponsesUsage(&original)
			require.NotNil(t, chat.PromptTokensDetails)
			require.Equal(t, 2000, chat.PromptTokensDetails.CachedTokens)
			encoded, err := json.Marshal(chat)
			require.NoError(t, err)
			require.NotContains(t, string(encoded), `"cache_creation_tokens":7000`)
			require.Contains(t, string(encoded), `"cache_write_tokens":0`)

			var decoded ChatUsage
			require.NoError(t, json.Unmarshal(encoded, &decoded))
			back := ChatUsageToResponsesUsage(&decoded)
			require.Zero(t, back.CacheCreationInputTokens)
			encoded, err = json.Marshal(back)
			require.NoError(t, err)
			require.Contains(t, string(encoded), `"cache_write_tokens":0`)
			var final ResponsesUsage
			require.NoError(t, json.Unmarshal(encoded, &final))
			require.Zero(t, final.CacheCreationInputTokens)
		})
	}
}

func TestChatCacheTTLUsageAggregatePresenceAcrossResponsesJSON(t *testing.T) {
	for _, tt := range []struct {
		name, details, wantField string
	}{
		{"canonical zero", `{"cache_write_tokens":0}`, `"cache_write_tokens":0`},
		{"legacy zero", `{"cache_creation_tokens":0}`, `"cache_creation_tokens":0`},
		{"canonical zero overrides legacy", `{"cache_write_tokens":0,"cache_creation_tokens":7000}`, `"cache_write_tokens":0`},
		{"missing aggregate", `{"cached_tokens":2}`, ``},
		{"missing aggregate with buckets", `{"cache_write_tokens_details":{"ttl_30m_tokens":3,"ttl_12h_tokens":4}}`, ``},
	} {
		t.Run(tt.name, func(t *testing.T) {
			var chat ChatUsage
			require.NoError(t, json.Unmarshal([]byte(`{"prompt_tokens":20,"prompt_tokens_details":`+tt.details+`}`), &chat))
			responses := ChatUsageToResponsesUsage(&chat)
			require.Zero(t, responses.CacheCreationInputTokens)
			encoded, err := json.Marshal(responses)
			require.NoError(t, err)
			require.NotContains(t, string(encoded), `"cache_creation_tokens":7000`)
			if tt.wantField == "" {
				require.NotContains(t, string(encoded), `"cache_write_tokens":0`)
				require.NotContains(t, string(encoded), `"cache_creation_tokens":0`)
			} else {
				require.Contains(t, string(encoded), tt.wantField)
			}
			var decoded ResponsesUsage
			require.NoError(t, json.Unmarshal(encoded, &decoded))
			encoded, err = json.Marshal(decoded)
			require.NoError(t, err)
			if tt.wantField != "" {
				require.Contains(t, string(encoded), tt.wantField)
			} else {
				require.NotContains(t, string(encoded), `"cache_write_tokens":0`)
				require.NotContains(t, string(encoded), `"cache_creation_tokens":0`)
			}
		})
	}
}

func TestResponsesPromptAggregateZeroPresenceAcrossChatJSON(t *testing.T) {
	for _, field := range []string{"cache_write_tokens", "cache_creation_tokens"} {
		t.Run(field, func(t *testing.T) {
			body := `{"input_tokens":20,"input_tokens_details":{"cached_tokens":2},"prompt_tokens_details":{"` + field + `":0}}`
			var usage ResponsesUsage
			require.NoError(t, json.Unmarshal([]byte(body), &usage))
			chat := chatUsageFromResponsesUsage(&usage)
			encoded, err := json.Marshal(chat)
			require.NoError(t, err)
			require.Contains(t, string(encoded), `"`+field+`":0`)
			var decoded ChatUsage
			require.NoError(t, json.Unmarshal(encoded, &decoded))
			responses := ChatUsageToResponsesUsage(&decoded)
			encoded, err = json.Marshal(responses)
			require.NoError(t, err)
			require.Contains(t, string(encoded), `"`+field+`":0`)
		})
	}
}

func TestResponsesCacheAggregateAliasesPreserveSelectedZeroAcrossJSON(t *testing.T) {
	for _, tt := range []struct {
		name, fields string
		wantTotal    int
		wantZero     bool
	}{
		{"creation input zero", `"cache_creation_input_tokens":0`, 0, true},
		{"write input zero", `"cache_write_input_tokens":0`, 0, true},
		{"write zero", `"cache_write_tokens":0`, 0, true},
		{"creation zero", `"cache_creation_tokens":0`, 0, true},
		{"zero exponent", `"cache_write_tokens":0e5`, 0, true},
		{"null", `"cache_write_tokens":null`, 0, false},
		{"missing", `"total_tokens":0`, 0, false},
		{"nested null blocks lower zero", `"input_tokens_details":{"cache_write_tokens":null},"prompt_tokens_details":{"cache_write_tokens":0}`, 0, false},
		{"nested null blocks lower positive", `"input_tokens_details":{"cache_write_tokens":null},"prompt_tokens_details":{"cache_write_tokens":7}`, 0, false},
		{"nested invalid blocks lower zero", `"input_tokens_details":{"cache_write_tokens":"invalid"},"prompt_tokens_details":{"cache_write_tokens":0}`, 0, false},
		{"top null blocks lower zero", `"cache_write_tokens":null,"cache_creation_tokens":0`, 0, false},
		{"higher positive beats lower zero", `"cache_write_tokens":7,"cache_creation_input_tokens":0`, 7, false},
		{"lower positive prevents zero", `"cache_write_tokens":0,"cache_write_input_tokens":7`, 7, false},
		{"nested positive beats top zero", `"input_tokens_details":{"cache_write_tokens":7},"cache_creation_input_tokens":0`, 7, false},
	} {
		t.Run(tt.name, func(t *testing.T) {
			var original ResponsesUsage
			require.NoError(t, json.Unmarshal([]byte(`{"input_tokens":0,`+tt.fields+`}`), &original))
			require.Equal(t, tt.wantTotal, original.CacheCreationInputTokens)
			chat := chatUsageFromResponsesUsage(&original)
			encoded, err := json.Marshal(chat)
			require.NoError(t, err)
			var decoded ChatUsage
			require.NoError(t, json.Unmarshal(encoded, &decoded))
			back := ChatUsageToResponsesUsage(&decoded)
			require.Equal(t, tt.wantTotal, back.CacheCreationInputTokens)
			encoded, err = json.Marshal(back)
			require.NoError(t, err)
			var final ResponsesUsage
			require.NoError(t, json.Unmarshal(encoded, &final))
			require.Equal(t, tt.wantTotal, final.CacheCreationInputTokens)
			zero := final.InputTokensDetails != nil && (final.InputTokensDetails.cacheWriteTokensExplicitZero || final.InputTokensDetails.cacheCreationTokensExplicitZero)
			require.Equal(t, tt.wantZero, zero)
		})
	}
}

func TestResponsesCacheTTLUsageCanonicalPositiveSuppressesLegacyAcrossJSON(t *testing.T) {
	var original ResponsesUsage
	body := `{"input_tokens":20,"input_tokens_details":{"cached_tokens":2,"cache_creation_tokens":7000},"prompt_tokens_details":{"cache_write_tokens":7}}`
	require.NoError(t, json.Unmarshal([]byte(body), &original))
	require.Equal(t, 7, original.CacheCreationInputTokens)
	chat := chatUsageFromResponsesUsage(&original)
	encoded, err := json.Marshal(chat)
	require.NoError(t, err)
	require.NotContains(t, string(encoded), `"cache_creation_tokens":7000`)
	var decoded ChatUsage
	require.NoError(t, json.Unmarshal(encoded, &decoded))
	back := ChatUsageToResponsesUsage(&decoded)
	require.Equal(t, 7, back.CacheCreationInputTokens)
	encoded, err = json.Marshal(back)
	require.NoError(t, err)
	var final ResponsesUsage
	require.NoError(t, json.Unmarshal(encoded, &final))
	require.Equal(t, 7, final.CacheCreationInputTokens)
}

func TestCacheTTLUsageMalformedDetailsPreserveAggregate(t *testing.T) {
	for _, tt := range []struct {
		name, details string
	}{
		{"null", `null`},
		{"array", `[]`},
		{"string count", `{"ttl_30m_tokens":"3","ttl_12h_tokens":4}`},
		{"fractional count", `{"ttl_30m_tokens":3.5,"ttl_12h_tokens":4}`},
		{"negative count", `{"ttl_30m_tokens":-1,"ttl_12h_tokens":8}`},
		{"partial", `{"ttl_12h_tokens":7}`},
		{"mismatched", `{"ttl_30m_tokens":4,"ttl_12h_tokens":4}`},
	} {
		t.Run(tt.name, func(t *testing.T) {
			var responses ResponsesUsage
			require.NoError(t, json.Unmarshal([]byte(`{"input_tokens":20,"input_tokens_details":{"cached_tokens":2,"cache_write_tokens":7,"cache_write_tokens_details":`+tt.details+`}}`), &responses))
			require.Equal(t, 7, responses.CacheCreationInputTokens)
			require.Equal(t, 2, responses.InputTokensDetails.CachedTokens)
			_, _, ok := responses.InputTokensDetails.CacheWriteTokensDetails.Counts(responses.CacheCreationInputTokens)
			require.False(t, ok)
			converted := chatUsageFromResponsesUsage(&responses)
			require.NotNil(t, converted.PromptTokensDetails)
			require.Nil(t, converted.PromptTokensDetails.CacheWriteTokensDetails)
			require.Equal(t, 7, ChatUsageToResponsesUsage(converted).CacheCreationInputTokens)

			var chat ChatUsage
			require.NoError(t, json.Unmarshal([]byte(`{"prompt_tokens":20,"prompt_tokens_details":{"cached_tokens":2,"cache_write_tokens":7,"cache_write_tokens_details":`+tt.details+`}}`), &chat))
			result := ChatUsageToResponsesUsage(&chat)
			require.Equal(t, 7, result.CacheCreationInputTokens)
			require.Equal(t, 2, result.InputTokensDetails.CachedTokens)
			require.Nil(t, result.InputTokensDetails.CacheWriteTokensDetails)
		})
	}
}

func TestCacheTTLUsageIntegralDecimalDetails(t *testing.T) {
	var usage ResponsesUsage
	require.NoError(t, json.Unmarshal([]byte(`{"input_tokens_details":{"cache_write_tokens":7,"cache_write_tokens_details":{"ttl_30m_tokens":3.0,"ttl_12h_tokens":4e0}}}`), &usage))
	short, long, ok := usage.InputTokensDetails.CacheWriteTokensDetails.Counts(usage.CacheCreationInputTokens)
	require.True(t, ok)
	require.Equal(t, 3, short)
	require.Equal(t, 4, long)
}

func TestCacheTTLUsagePromptDetailsWithSeparateInputDetails(t *testing.T) {
	var usage ResponsesUsage
	require.NoError(t, json.Unmarshal([]byte(`{"input_tokens_details":{"cached_tokens":2},"prompt_tokens_details":{"cache_write_tokens":7,"cache_write_tokens_details":{"ttl_30m_tokens":3,"ttl_12h_tokens":4}}}`), &usage))
	require.Equal(t, 2, usage.InputTokensDetails.CachedTokens)
	require.Equal(t, 7, usage.CacheCreationInputTokens)
	short, long, ok := usage.InputTokensDetails.CacheWriteTokensDetails.Counts(usage.CacheCreationInputTokens)
	require.True(t, ok)
	require.Equal(t, 3, short)
	require.Equal(t, 4, long)
}
