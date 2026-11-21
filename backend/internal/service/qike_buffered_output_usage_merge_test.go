//go:build unit

package service

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestQikeBufferedTerminalReconstructsHostedOutputAndPreservesUsage(t *testing.T) {
	gin.SetMode(gin.TestMode)
	const observed = `{"input_tokens":12000,"output_tokens":10,"input_tokens_details":{"cached_tokens":4000,"cache_write_tokens":7000,"cache_write_tokens_details":{"ttl_30m_tokens":3000,"ttl_12h_tokens":4000}}}`
	cases := []struct {
		name, terminal             string
		input, output, cache, s, l int
	}{
		{"empty terminal", `{}`, 12000, 10, 7000, 3000, 4000},
		{"explicit cache zero", `{"input_tokens_details":{"cache_write_tokens":0}}`, 12000, 10, 0, 0, 0},
		{"positive terminal snapshot", `{"input_tokens":9,"output_tokens":2,"input_tokens_details":{"cache_write_tokens":6,"cache_write_tokens_details":{"ttl_30m_tokens":2,"ttl_12h_tokens":4}}}`, 9, 2, 6, 2, 4},
	}
	for _, tc := range cases {
		for _, eof := range []bool{false, true} {
			name := tc.name + "/complete-frame"
			if eof {
				name = tc.name + "/unterminated-eof"
			}
			t.Run(name, func(t *testing.T) {
				sse := bufferedItemsSSE(
					bufferedHostedItemsCreated,
					`{"type":"response.in_progress","response":{"usage":`+observed+`}}`,
					bufferedHostedItemsWebSearchDone,
					`{"type":"response.completed","response":{"id":"resp_1","status":"completed","output":[],"usage":`+tc.terminal+`}}`,
				)
				if eof {
					sse = strings.TrimSuffix(sse, "\n\n")
				}
				c, _ := gin.CreateTestContext(httptest.NewRecorder())
				c.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
				account := &Account{ID: 1, Platform: PlatformOpenAI, Type: AccountTypeOAuth, Concurrency: 1}
				response, usage, acc, _, err := (&OpenAIGatewayService{}).readOpenAICompatBufferedTerminal(
					bufferedHostedItemsSSEResponse(sse), c, account, "qike buffered test", "req-test")
				require.NoError(t, err)
				require.NotNil(t, response)
				require.Len(t, response.Output, 1)
				require.Equal(t, "web_search_call", response.Output[0].Type)
				acc.SupplementResponseOutput(response)
				require.Len(t, response.Output, 1, "hosted output should not be duplicated")
				require.Equal(t, tc.input, usage.InputTokens)
				require.Equal(t, tc.output, usage.OutputTokens)
				require.Equal(t, tc.cache, usage.CacheCreationInputTokens)
				require.Equal(t, tc.s, usage.CacheCreation30mInputTokens)
				require.Equal(t, tc.l, usage.CacheCreation12hInputTokens)
				require.NotNil(t, response.Usage)
				require.Equal(t, tc.input, response.Usage.InputTokens)
				require.Equal(t, tc.output, response.Usage.OutputTokens)
				require.Equal(t, tc.cache, response.Usage.CacheCreationInputTokens)
			})
		}
	}
}
