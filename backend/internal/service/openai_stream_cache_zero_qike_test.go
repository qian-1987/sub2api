package service

import "testing"

func TestQikeStreamCacheWriteExplicitZero(t *testing.T) {
	cases := []struct {
		name       string
		usage      string
		clearCache bool
	}{
		{"input zero", `{"input_tokens_details":{"cache_write_tokens":0}}`, true},
		{"prompt zero", `{"prompt_tokens_details":{"cache_write_tokens":0}}`, true},
		{"input creation zero", `{"input_tokens_details":{"cache_creation_tokens":0}}`, true},
		{"prompt creation zero", `{"prompt_tokens_details":{"cache_creation_tokens":0}}`, true},
		{"top level zero", `{"cache_write_tokens":0}`, true},
		{"top level creation input zero", `{"cache_creation_input_tokens":0}`, true},
		{"top level write input zero", `{"cache_write_input_tokens":0}`, true},
		{"top level creation zero", `{"cache_creation_tokens":0}`, true},
		{"decimal zero", `{"cache_creation_input_tokens":0.0}`, true},
		{"exponent zero", `{"cache_write_input_tokens":-0e-999}`, true},
		{"nested zero beats top positive", `{"input_tokens_details":{"cache_creation_tokens":0},"cache_write_tokens":7}`, true},
		{"missing", `{"input_tokens":0,"output_tokens":0}`, false},
		{"null", `{"input_tokens_details":{"cache_write_tokens":null}}`, false},
		{"string", `{"input_tokens_details":{"cache_write_tokens":"0"}}`, false},
		{"negative", `{"input_tokens_details":{"cache_write_tokens":-1}}`, false},
		{"fractional", `{"input_tokens_details":{"cache_write_tokens":0.1}}`, false},
		{"positive underflow", `{"input_tokens_details":{"cache_write_tokens":1e-999}}`, false},
		{"negative underflow", `{"cache_write_input_tokens":-1e-999}`, false},
		{"nested creation null", `{"input_tokens_details":{"cache_creation_tokens":null}}`, false},
		{"nested creation string", `{"prompt_tokens_details":{"cache_creation_tokens":"0"}}`, false},
		{"nested creation negative", `{"input_tokens_details":{"cache_creation_tokens":-1}}`, false},
		{"top creation string", `{"cache_creation_input_tokens":"0"}`, false},
		{"top write null", `{"cache_write_input_tokens":null}`, false},
		{"top creation negative", `{"cache_creation_tokens":-1}`, false},
		{"invalid nested beats later zero", `{"input_tokens_details":{"cache_creation_tokens":null},"cache_write_tokens":0}`, false},
		{"lower priority zero", `{"input_tokens":12,"output_tokens":2,"input_tokens_details":{"cache_creation_tokens":7},"cache_write_tokens":0}`, false},
		{"top zero with positive alias", `{"input_tokens":12,"output_tokens":2,"cache_write_tokens":0,"cache_creation_input_tokens":7}`, false},
	}
	for _, transport := range []string{"sse", "ws"} {
		for _, event := range []string{"response.in_progress", "response.completed"} {
			for _, tc := range cases {
				t.Run(transport+"/"+event+"/"+tc.name, func(t *testing.T) {
					usage := &OpenAIUsage{InputTokens: 12, OutputTokens: 2, CacheCreationInputTokens: 7, CacheCreation30mInputTokens: 3, CacheCreation12hInputTokens: 4}
					body := `{"type":"` + event + `","response":{"usage":` + tc.usage + `}}`
					if transport == "sse" {
						(&OpenAIGatewayService{}).parseSSEUsage(body, usage)
					} else {
						parseOpenAIWSResponseUsageFromCompletedEvent([]byte(body), usage)
					}
					if usage.InputTokens != 12 || usage.OutputTokens != 2 {
						t.Fatalf("other counters changed: %+v", usage)
					}
					if tc.clearCache {
						if usage.CacheCreationInputTokens != 0 || usage.CacheCreation30mInputTokens != 0 || usage.CacheCreation12hInputTokens != 0 {
							t.Fatalf("explicit zero did not clear cache counters: %+v", usage)
						}
					} else if usage.CacheCreationInputTokens != 7 {
						t.Fatalf("non-authoritative cache value cleared prior write: %+v", usage)
					}
				})
			}
		}
	}
}

func TestQikeStreamUsesSelectedUsageForExplicitZero(t *testing.T) {
	for _, transport := range []string{"sse", "ws"} {
		t.Run(transport, func(t *testing.T) {
			usage := &OpenAIUsage{InputTokens: 12, CacheCreationInputTokens: 7, CacheCreation30mInputTokens: 3, CacheCreation12hInputTokens: 4}
			// The parser prefers the top-level usage; response.usage must not
			// independently clear it.
			body := `{"type":"response.in_progress","usage":{},"response":{"usage":{"input_tokens_details":{"cache_write_tokens":0}}}}`
			if transport == "sse" {
				(&OpenAIGatewayService{}).parseSSEUsage(body, usage)
			} else {
				parseOpenAIWSResponseUsageFromCompletedEvent([]byte(body), usage)
			}
			if usage.CacheCreationInputTokens != 7 || usage.CacheCreation30mInputTokens != 3 || usage.CacheCreation12hInputTokens != 4 {
				t.Fatalf("lower-priority usage changed the selected usage: %+v", usage)
			}
		})
	}
}

func TestQikeStreamNonzeroTerminalRemainsAuthoritative(t *testing.T) {
	for _, transport := range []string{"sse", "ws"} {
		for _, fields := range []string{"", `,"input_tokens_details":{"cache_creation_tokens":null}`} {
			t.Run(transport+"/"+fields, func(t *testing.T) {
				usage := &OpenAIUsage{InputTokens: 12, OutputTokens: 2, CacheCreationInputTokens: 7, CacheCreation30mInputTokens: 3, CacheCreation12hInputTokens: 4}
				body := `{"type":"response.completed","response":{"usage":{"input_tokens":19,"output_tokens":7` + fields + `}}}`
				if transport == "sse" {
					(&OpenAIGatewayService{}).parseSSEUsage(body, usage)
				} else {
					parseOpenAIWSResponseUsageFromCompletedEvent([]byte(body), usage)
				}
				if usage.InputTokens != 19 || usage.OutputTokens != 7 || usage.CacheCreationInputTokens != 0 || usage.CacheCreation30mInputTokens != 0 || usage.CacheCreation12hInputTokens != 0 {
					t.Fatalf("nonzero terminal usage must keep upstream replacement behavior: %+v", usage)
				}
			})
		}
	}
}
