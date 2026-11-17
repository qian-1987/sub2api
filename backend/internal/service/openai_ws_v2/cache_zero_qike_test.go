package openai_ws_v2

import "testing"

func TestQikeRelayCacheWriteExplicitZero(t *testing.T) {
	cases := []struct {
		name       string
		fields     string
		clearCache bool
	}{
		{"input zero", `"input_tokens_details":{"cache_write_tokens":0}`, true},
		{"prompt zero", `"prompt_tokens_details":{"cache_write_tokens":0}`, true},
		{"input creation zero", `"input_tokens_details":{"cache_creation_tokens":0}`, true},
		{"prompt creation zero", `"prompt_tokens_details":{"cache_creation_tokens":0}`, true},
		{"top level zero", `"cache_write_tokens":0`, true},
		{"top creation input zero", `"cache_creation_input_tokens":0`, true},
		{"top write input zero", `"cache_write_input_tokens":0`, true},
		{"top creation zero", `"cache_creation_tokens":0`, true},
		{"decimal zero", `"cache_creation_input_tokens":0.0`, true},
		{"exponent zero", `"cache_write_input_tokens":-0e-999`, true},
		{"nested zero beats top positive", `"input_tokens_details":{"cache_creation_tokens":0},"cache_write_tokens":7`, true},
		{"missing", `"total_tokens":0`, false},
		{"null", `"input_tokens_details":{"cache_write_tokens":null}`, false},
		{"string", `"input_tokens_details":{"cache_write_tokens":"0"}`, false},
		{"negative", `"input_tokens_details":{"cache_write_tokens":-1}`, false},
		{"fractional", `"input_tokens_details":{"cache_write_tokens":0.1}`, false},
		{"positive underflow", `"input_tokens_details":{"cache_write_tokens":1e-999}`, false},
		{"negative underflow", `"cache_write_input_tokens":-1e-999`, false},
		{"nested creation null", `"input_tokens_details":{"cache_creation_tokens":null}`, false},
		{"nested creation string", `"prompt_tokens_details":{"cache_creation_tokens":"0"}`, false},
		{"nested creation negative", `"input_tokens_details":{"cache_creation_tokens":-1}`, false},
		{"top creation string", `"cache_creation_input_tokens":"0"`, false},
		{"top write null", `"cache_write_input_tokens":null`, false},
		{"top creation negative", `"cache_creation_tokens":-1`, false},
		{"invalid nested beats later zero", `"input_tokens_details":{"cache_creation_tokens":null},"cache_write_tokens":0`, false},
		{"invalid top alias beats later zero", `"cache_write_tokens":"0","cache_creation_input_tokens":0`, false},
	}
	for _, event := range []string{"response.in_progress", "response.completed"} {
		for _, tc := range cases {
			t.Run(event+"/"+tc.name, func(t *testing.T) {
				state := &relayState{turnUsage: Usage{InputTokens: 12, OutputTokens: 2, CacheCreationInputTokens: 7, CacheCreation30mInputTokens: 3, CacheCreation12hInputTokens: 4}}
				message := `{"response":{"usage":{"input_tokens":0,"output_tokens":0,` + tc.fields + `}}}`
				parseUsageAndAccumulate(state, []byte(message), event, nil)
				got := state.turnUsage
				if got.InputTokens != 12 || got.OutputTokens != 2 {
					t.Fatalf("other counters changed: %+v", got)
				}
				if tc.clearCache {
					if got.CacheCreationInputTokens != 0 || got.CacheCreation30mInputTokens != 0 || got.CacheCreation12hInputTokens != 0 {
						t.Fatalf("explicit zero did not clear cache counters: %+v", got)
					}
				} else if got.CacheCreationInputTokens != 7 || got.CacheCreation30mInputTokens != 3 || got.CacheCreation12hInputTokens != 4 {
					t.Fatalf("invalid or missing cache value changed the previous snapshot: %+v", got)
				}
			})
		}
	}
}

func TestQikeRelayPositiveAliasWinsOverTopLevelZero(t *testing.T) {
	for _, event := range []string{"response.in_progress", "response.completed"} {
		t.Run(event, func(t *testing.T) {
			state := &relayState{turnUsage: Usage{InputTokens: 12, OutputTokens: 2, CacheCreationInputTokens: 7}}
			message := []byte(`{"response":{"usage":{"input_tokens":12,"output_tokens":2,"cache_write_tokens":0,"cache_creation_input_tokens":7}}}`)
			parseUsageAndAccumulate(state, message, event, nil)
			if state.turnUsage.CacheCreationInputTokens != 7 {
				t.Fatalf("nonzero aggregate must win over a zero fallback alias: %+v", state.turnUsage)
			}
		})
	}
}

func TestQikeRelayNonzeroTerminalRemainsAuthoritative(t *testing.T) {
	for _, fields := range []string{"", `,"input_tokens_details":{"cache_creation_tokens":null}`} {
		t.Run(fields, func(t *testing.T) {
			state := &relayState{turnUsage: Usage{InputTokens: 12, OutputTokens: 2, CacheCreationInputTokens: 7, CacheCreation30mInputTokens: 3, CacheCreation12hInputTokens: 4}}
			message := []byte(`{"response":{"usage":{"input_tokens":19,"output_tokens":7` + fields + `}}}`)
			parseUsageAndAccumulate(state, message, "response.completed", nil)
			got := state.turnUsage
			if got.InputTokens != 19 || got.OutputTokens != 7 || got.CacheCreationInputTokens != 0 || got.CacheCreation30mInputTokens != 0 || got.CacheCreation12hInputTokens != 0 {
				t.Fatalf("nonzero terminal usage must keep upstream replacement behavior: %+v", got)
			}
		})
	}
}

func TestQikeRelayCacheWriteBreakdownRequiresAggregate(t *testing.T) {
	state := &relayState{}
	message := []byte(`{"response":{"usage":{"input_tokens":12,"output_tokens":2,"input_tokens_details":{"cache_write_tokens_details":{"ttl_30m_tokens":3,"ttl_12h_tokens":4}}}}}`)
	parseUsageAndAccumulate(state, message, "response.completed", nil)
	got := finalizeRelayTurnUsage(state)
	if got.CacheCreationInputTokens != 0 || got.CacheCreation30mInputTokens != 0 || got.CacheCreation12hInputTokens != 0 {
		t.Fatalf("TTL buckets must not invent the aggregate: %+v", got)
	}
}

func TestQikeRelayCacheWriteBreakdownRejectsInvalidDetails(t *testing.T) {
	for _, details := range []string{
		`{}`,
		`{"ttl_30m_tokens":3}`,
		`{"ttl_30m_tokens":3,"ttl_12h_tokens":3}`,
		`{"ttl_30m_tokens":-1,"ttl_12h_tokens":8}`,
		`{"ttl_30m_tokens":3.5,"ttl_12h_tokens":3.5}`,
		`{"ttl_30m_tokens":"3","ttl_12h_tokens":"4"}`,
	} {
		t.Run(details, func(t *testing.T) {
			state := &relayState{}
			message := []byte(`{"response":{"usage":{"input_tokens":12,"output_tokens":2,"input_tokens_details":{"cache_write_tokens":7,"cache_write_tokens_details":` + details + `}}}}`)
			parseUsageAndAccumulate(state, message, "response.completed", nil)
			got := finalizeRelayTurnUsage(state)
			if got.CacheCreationInputTokens != 7 || got.CacheCreation30mInputTokens != 0 || got.CacheCreation12hInputTokens != 0 {
				t.Fatalf("invalid TTL details should keep only the aggregate: %+v", got)
			}
		})
	}
}
