//go:build unit

package service

import (
	"context"
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

func TestQikeDualTerminalUsageKeepsSelectedCacheBuckets(t *testing.T) {
	gin.SetMode(gin.TestMode)
	const topZero = `{"input_tokens":12000,"output_tokens":10,"input_tokens_details":{"cached_tokens":4000,"cache_write_tokens":0}}`
	const topSix = `{"input_tokens":12000,"output_tokens":10,"input_tokens_details":{"cached_tokens":4000,"cache_write_tokens":6000,"cache_write_tokens_details":{"ttl_30m_tokens":2000,"ttl_12h_tokens":4000}}}`
	const nestedSeven = `{"input_tokens":12000,"output_tokens":10,"input_tokens_details":{"cached_tokens":4000,"cache_write_tokens":7000,"cache_write_tokens_details":{"ttl_30m_tokens":3000,"ttl_12h_tokens":4000}}}`
	const nestedZero = `{"input_tokens":12000,"output_tokens":10,"input_tokens_details":{"cached_tokens":4000,"cache_write_tokens":0,"cache_write_tokens_details":{"ttl_30m_tokens":3000,"ttl_12h_tokens":4000}}}`
	for _, tc := range []struct {
		name, top, nested     string
		creation, short, long int
	}{
		{"nested mixed buckets override top zero", topZero, nestedSeven, 7000, 3000, 4000},
		{"nested mixed buckets override top positive", topSix, nestedSeven, 7000, 3000, 4000},
		{"nested explicit zero overrides top positive and conflicting buckets", topSix, nestedZero, 0, 0, 0},
		{"top level used when nested absent", topSix, "", 6000, 2000, 4000},
	} {
		for _, stream := range []bool{false, true} {
			name := tc.name + "/buffered"
			if stream {
				name = tc.name + "/streaming"
			}
			t.Run(name, func(t *testing.T) {
				nestedUsage := ""
				if tc.nested != "" {
					nestedUsage = `,"usage":` + tc.nested
				}
				payload := `data: {"type":"response.created","response":{"id":"resp_cache","model":"gpt-6.1-terra"}}` + "\n\n" +
					`data: {"type":"response.completed","usage":` + tc.top + `,"response":{"id":"resp_cache","status":"completed","output":[]` + nestedUsage + `}}` + "\n\n"
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
				require.Equal(t, tc.creation, result.Usage.CacheCreationInputTokens)
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
				require.EqualValues(t, tc.creation, clientUsage.Get("cache_creation_input_tokens").Int())

				usageRepo := &openAIRecordUsageLogRepoStub{inserted: true}
				billingSvc := newOpenAIRecordUsageServiceForTest(usageRepo, &openAIRecordUsageUserRepoStub{}, &openAIRecordUsageSubRepoStub{}, nil)
				billingSvc.billingService = NewBillingService(billingSvc.cfg, &PricingService{pricingData: map[string]*LiteLLMModelPricing{
					"gpt-6.1-terra": {InputCostPerToken: 1e-6, OutputCostPerToken: 5e-6, CacheReadInputTokenCost: .05e-6},
				}})
				result.RequestID = "qike-dual-terminal-test"
				require.NoError(t, billingSvc.RecordUsage(context.Background(), &OpenAIRecordUsageInput{
					Result: result, APIKey: &APIKey{ID: 1056}, User: &User{ID: 2056}, Account: &Account{ID: 3056, Platform: PlatformOpenAI},
				}))
				require.NotNil(t, usageRepo.lastLog)
				require.Equal(t, tc.creation, usageRepo.lastLog.CacheCreationTokens)
				require.Equal(t, tc.short, usageRepo.lastLog.CacheCreation30mTokens)
				require.Equal(t, tc.long, usageRepo.lastLog.CacheCreation12hTokens)
				require.Equal(t, 8000-tc.creation, usageRepo.lastLog.InputTokens)
			})
		}
	}
}
