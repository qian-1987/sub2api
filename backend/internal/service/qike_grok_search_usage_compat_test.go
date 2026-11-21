//go:build unit

package service

import (
	"fmt"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// These tests cover Responses-compatible providers that put terminal usage at
// the event top level or describe the SSE event type on the event: line.
func TestQikeGrokSearchUsageTerminalCompatibility(t *testing.T) {
	account := healthyGrokOAuthGatewayTestAccount(4251, "access-token")
	tests := []struct {
		name, nested, top string
		want              int
	}{
		{"top-level fallback with nested token usage", `{"input_tokens":5,"output_tokens":2}`, `{"x_search_calls":4}`, 4},
		{"top-level fallback with empty nested usage", `{}`, `{"x_search_calls":4}`, 4},
		{"top-level fallback with null nested detail", `{"server_side_tool_usage_details":null}`, `{"x_search_calls":4}`, 4},
		{"empty nested detail is authoritative", `{"server_side_tool_usage_details":{}}`, `{"x_search_calls":4}`, 0},
		{"explicit nested zero is authoritative", `{"server_side_tool_usage_details":{"x_search_calls":0}}`, `{"x_search_calls":4}`, 0},
		{"nested count wins", `{"server_side_tool_usage_details":{"x_search_calls":2}}`, `{"x_search_calls":4}`, 2},
		{"fallback to output items", `{"input_tokens":5,"output_tokens":2}`, `null`, 1},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			base := grokCustomToolCallStream([]string{grokXSearchItemJSON}, `[]`)
			at := strings.LastIndex(base, `data: {"type":"response.completed"`)
			require.NotEqual(t, -1, at)
			terminal := fmt.Sprintf(`{"type":"response.completed","response":{"id":"resp_1","status":"completed","output":[],"usage":%s},"usage":{"input_tokens":5,"output_tokens":2,"server_side_tool_usage_details":%s}}`, tc.nested, tc.top)
			sse := base[:at] + "data: " + terminal + "\n\n"
			require.Equal(t, tc.want, countGrokNativeSearchCallsFromSSEBody(sse), "SSE aggregation")
			require.Equal(t, tc.want, forwardGrokResponsesSearchCount(t, true, sse), "Responses streaming")
			for _, path := range []string{"/v1/messages", "/v1/chat/completions"} {
				for _, streaming := range []bool{false, true} {
					t.Run(fmt.Sprintf("%s stream=%v", path, streaming), func(t *testing.T) {
						got := forwardGrokCustomToolCallStream(t, path, streaming, account, sse)
						require.Equal(t, tc.want, got.searchCount)
					})
				}
			}
		})
	}
}

func TestQikeGrokSearchEventLineWithoutPayloadType(t *testing.T) {
	item := `{"type":"web_search_call","status":"completed"}`
	sse := "event: response.created\ndata: {\"response\":{\"id\":\"resp_1\",\"model\":\"grok-4.7\"}}\n\n" +
		"event: response.output_item.added\ndata: {\"item\":" + item + "}\n\n" +
		"event: response.output_item.done\ndata: {\"item\":" + item + "}\n\n" +
		"event: response.output_item.done\ndata: {\"item\":" + item + "}\n\n" +
		"event: response.completed\ndata: {\"response\":{\"id\":\"resp_1\",\"status\":\"completed\",\"output\":[" + item + "," + item + "],\"usage\":{\"input_tokens\":5,\"output_tokens\":2}}}\n\n"
	require.Equal(t, 2, countGrokNativeSearchCallsFromSSEBody(sse))
	require.Equal(t, 2, forwardGrokResponsesSearchCount(t, true, sse))
	usageSSE := strings.Replace(sse, `"output_tokens":2`, `"output_tokens":2,"server_side_tool_usage_details":{"web_search_calls":4}`, 1)
	require.Equal(t, 4, countGrokNativeSearchCallsFromSSEBody(usageSSE))
	require.Equal(t, 4, forwardGrokResponsesSearchCount(t, true, usageSSE))
}
