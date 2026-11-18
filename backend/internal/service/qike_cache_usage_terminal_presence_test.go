//go:build unit

package service

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

// Empty or null cache usage must not discard already observed counts; a
// numeric zero is authoritative and clears only the cache-write buckets.
func TestQikeEmptyTerminalUsagePreservesEarlierCache(t *testing.T) {
	gin.SetMode(gin.TestMode)
	const observed = `{"input_tokens":12000,"output_tokens":10,"input_tokens_details":{"cached_tokens":4000,"cache_write_tokens":7000,"cache_write_tokens_details":{"ttl_30m_tokens":3000,"ttl_12h_tokens":4000}}}`
	for _, tc := range []struct {
		name, nested      string
		want, short, long int
	}{
		{"empty", `{}`, 7000, 3000, 4000},
		{"null aggregate", `{"input_tokens_details":{"cache_write_tokens":null}}`, 7000, 3000, 4000},
		{"explicit zero", `{"input_tokens_details":{"cache_write_tokens":0}}`, 0, 0, 0},
	} {
		for _, source := range []string{"top-level terminal", "progressive snapshot"} {
			for _, stream := range []bool{false, true} {
				name := tc.name + "/" + source + "/buffered"
				if stream {
					name = tc.name + "/" + source + "/streaming"
				}
				t.Run(name, func(t *testing.T) {
					payload := `data: {"type":"response.created","response":{"id":"resp_cache","model":"gpt-6.1-terra"}}` + "\n\n"
					top := `,"usage":` + observed
					if source == "progressive snapshot" {
						payload += `data: {"type":"response.in_progress","response":{"usage":` + observed + `}}` + "\n\n"
						top = ""
					}
					payload += `data: {"type":"response.completed"` + top + `,"response":{"id":"resp_cache","status":"completed","output":[],"usage":` + tc.nested + `}}` + "\n\n"
					recorder := httptest.NewRecorder()
					ctx, _ := gin.CreateTestContext(recorder)
					ctx.Request = httptest.NewRequest(http.MethodPost, "/v1/messages", nil)
					response := &http.Response{StatusCode: http.StatusOK, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(payload))}
					service := &OpenAIGatewayService{cfg: rawChatCompletionsTestConfig()}
					var result *OpenAIForwardResult
					var err error
					if stream {
						result, err = service.handleAnthropicStreamingResponse(response, ctx, rawChatCompletionsTestAccount(), "gpt-6.1-terra", "gpt-6.1-terra", "gpt-6.1-terra", time.Now())
					} else {
						result, err = service.handleAnthropicBufferedStreamingResponse(response, ctx, rawChatCompletionsTestAccount(), "gpt-6.1-terra", "gpt-6.1-terra", "gpt-6.1-terra", time.Now())
					}
					require.NoError(t, err)
					require.NotNil(t, result)
					require.Equal(t, 12000, result.Usage.InputTokens)
					require.Equal(t, 10, result.Usage.OutputTokens)
					require.Equal(t, 4000, result.Usage.CacheReadInputTokens)
					require.Equal(t, tc.want, result.Usage.CacheCreationInputTokens)
					require.Equal(t, tc.short, result.Usage.CacheCreation30mInputTokens)
					require.Equal(t, tc.long, result.Usage.CacheCreation12hInputTokens)
					clientUsage := gjson.Get(recorder.Body.String(), "usage")
					if stream {
						for _, line := range strings.Split(recorder.Body.String(), "\n") {
							if strings.HasPrefix(line, "data: ") {
								event := gjson.Parse(strings.TrimPrefix(line, "data: "))
								if event.Get("type").String() == "message_delta" {
									clientUsage = event.Get("usage")
								}
							}
						}
					}
					require.True(t, clientUsage.Exists())
					require.EqualValues(t, tc.want, clientUsage.Get("cache_creation_input_tokens").Int())
					require.EqualValues(t, 8000-tc.want, clientUsage.Get("input_tokens").Int())
					require.EqualValues(t, 4000, clientUsage.Get("cache_read_input_tokens").Int())
				})
			}
		}
	}
}
