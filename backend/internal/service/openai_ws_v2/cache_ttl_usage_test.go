package openai_ws_v2

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestRelayCacheTTLUsageSnapshotsAndTurns(t *testing.T) {
	state := &relayState{}
	mixed := []byte(`{"type":"response.completed","response":{"usage":{"input_tokens":12000,"output_tokens":10,"input_tokens_details":{"cached_tokens":4000,"cache_write_tokens":7000,"cache_write_tokens_details":{"ttl_30m_tokens":3000,"ttl_12h_tokens":4000}}}}}`)
	parseUsageAndAccumulate(state, mixed, "response.in_progress", nil)
	parseUsageAndAccumulate(state, mixed, "response.completed", nil)
	parseUsageAndAccumulate(state, mixed, "response.completed", nil)
	turn := finalizeRelayTurnUsage(state)
	require.Equal(t, 7000, turn.CacheCreationInputTokens)
	require.Equal(t, 3000, turn.CacheCreation30mInputTokens)
	require.Equal(t, 4000, turn.CacheCreation12hInputTokens)
	require.True(t, turn.CacheCreationBreakdownPresent)
	require.Equal(t, turn, state.usage)

	shortOnly := []byte(`{"type":"response.completed","response":{"usage":{"input_tokens":8000,"output_tokens":10,"input_tokens_details":{"cache_write_tokens":7000,"cache_write_tokens_details":{"ttl_30m_tokens":7000,"ttl_12h_tokens":0}}}}}`)
	parseUsageAndAccumulate(state, shortOnly, "response.completed", nil)
	turn = finalizeRelayTurnUsage(state)
	require.Equal(t, 7000, turn.CacheCreation30mInputTokens)
	require.Zero(t, turn.CacheCreation12hInputTokens)
	require.Equal(t, 14000, state.usage.CacheCreationInputTokens)
	require.Equal(t, 10000, state.usage.CacheCreation30mInputTokens)
	require.Equal(t, 4000, state.usage.CacheCreation12hInputTokens)
}

func TestRelayCacheTTLUsageExplicitZeroClearsProgressiveWrites(t *testing.T) {
	for _, event := range []string{"response.in_progress", "response.completed"} {
		t.Run(event, func(t *testing.T) {
			state := &relayState{}
			parseUsageAndAccumulate(state, []byte(`{"response":{"usage":{"input_tokens":12000,"output_tokens":10,"input_tokens_details":{"cache_write_tokens":7000,"cache_write_tokens_details":{"ttl_30m_tokens":3000,"ttl_12h_tokens":4000}}}}}`), "response.in_progress", nil)
			parseUsageAndAccumulate(state, []byte(`{"response":{"usage":{"input_tokens":0,"output_tokens":0}}}`), event, nil)
			require.Equal(t, 7000, state.turnUsage.CacheCreationInputTokens)
			parseUsageAndAccumulate(state, []byte(`{"response":{"usage":{"input_tokens":0,"output_tokens":0,"input_tokens_details":{"cache_write_tokens":0}}}}`), event, nil)
			turn := finalizeRelayTurnUsage(state)
			require.Zero(t, turn.CacheCreationInputTokens)
			require.Zero(t, turn.CacheCreation30mInputTokens)
			require.Zero(t, turn.CacheCreation12hInputTokens)
			require.False(t, turn.CacheCreationBreakdownPresent)
			require.Equal(t, 12000, turn.InputTokens)
		})
	}
}
