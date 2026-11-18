package apicompat

import (
	"encoding/json"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/pkg/openai"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

// A present null or negative canonical field blocks the legacy alias, but it
// must not become an explicit numeric zero that clears a streaming snapshot.
func TestChatCacheCanonicalPresenceDoesNotInventExplicitZero(t *testing.T) {
	for _, test := range []struct {
		name         string
		details      string
		wantExplicit bool
		wantChatWire string
	}{
		{"null overrides positive legacy", `"cache_write_tokens":null,"cache_creation_tokens":4`, false, `"cache_write_tokens":null`},
		{"null overrides legacy zero", `"cache_write_tokens":null,"cache_creation_tokens":0`, false, `"cache_write_tokens":null`},
		{"negative overrides legacy zero", `"cache_write_tokens":-2,"cache_creation_tokens":0`, false, `"cache_write_tokens":-2`},
		{"numeric zero overrides positive legacy", `"cache_write_tokens":0,"cache_creation_tokens":4`, true, `"cache_write_tokens":0`},
	} {
		t.Run(test.name, func(t *testing.T) {
			var chat ChatUsage
			payload := `{"prompt_tokens":20,"prompt_tokens_details":{` + test.details + `}}`
			require.NoError(t, json.Unmarshal([]byte(payload), &chat))
			encoded, err := json.Marshal(chat)
			require.NoError(t, err)
			require.Contains(t, string(encoded), test.wantChatWire)

			var decoded ChatUsage
			require.NoError(t, json.Unmarshal(encoded, &decoded))
			responses := ChatUsageToResponsesUsage(&decoded)
			require.Zero(t, responses.CacheCreationInputTokens)
			encoded, err = json.Marshal(responses)
			require.NoError(t, err)
			require.Equal(t, test.wantExplicit, openai.CacheWriteAggregateExplicitZero(gjson.ParseBytes(encoded), responses.CacheCreationInputTokens))
		})
	}
}
