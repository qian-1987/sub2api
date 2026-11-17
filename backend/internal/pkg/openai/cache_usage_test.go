package openai

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

func TestCacheWriteBreakdownFromUsage(t *testing.T) {
	tests := []struct {
		name, body                    string
		total, wantTotal, short, long int
		ok                            bool
	}{
		{"mixed", `{"input_tokens_details":{"cache_write_tokens":7000,"cache_write_tokens_details":{"ttl_30m_tokens":3000,"ttl_12h_tokens":4000}}}`, 7000, 7000, 3000, 4000, true},
		{"30m only", `{"input_tokens_details":{"cache_write_tokens":7000,"cache_write_tokens_details":{"ttl_30m_tokens":7000,"ttl_12h_tokens":0}}}`, 7000, 7000, 7000, 0, true},
		{"12h only", `{"input_tokens_details":{"cache_write_tokens":7000,"cache_write_tokens_details":{"ttl_30m_tokens":0,"ttl_12h_tokens":7000}}}`, 7000, 7000, 0, 7000, true},
		{"zero", `{"input_tokens_details":{"cache_write_tokens":0,"cache_write_tokens_details":{"ttl_30m_tokens":0,"ttl_12h_tokens":0}}}`, 0, 0, 0, 0, true},
		{"recover missing aggregate", `{"input_tokens_details":{"cache_write_tokens_details":{"ttl_30m_tokens":3000,"ttl_12h_tokens":4000}}}`, 0, 7000, 3000, 4000, true},
		{"aggregate only", `{"input_tokens_details":{"cache_write_tokens":7000}}`, 7000, 7000, 0, 0, false},
		{"legacy alias", `{"cache_creation_input_tokens":7000}`, 7000, 7000, 0, 0, false},
		{"partial", `{"input_tokens_details":{"cache_write_tokens":7000,"cache_write_tokens_details":{"ttl_12h_tokens":7000}}}`, 7000, 7000, 0, 0, false},
		{"negative", `{"input_tokens_details":{"cache_write_tokens":7000,"cache_write_tokens_details":{"ttl_30m_tokens":-1,"ttl_12h_tokens":7001}}}`, 7000, 7000, 0, 0, false},
		{"fractional", `{"input_tokens_details":{"cache_write_tokens":7,"cache_write_tokens_details":{"ttl_30m_tokens":3.5,"ttl_12h_tokens":3.5}}}`, 7, 7, 0, 0, false},
		{"too large", `{"input_tokens_details":{"cache_write_tokens":7000,"cache_write_tokens_details":{"ttl_30m_tokens":7000,"ttl_12h_tokens":7000}}}`, 7000, 7000, 0, 0, false},
		{"explicit aggregate zero", `{"input_tokens_details":{"cache_write_tokens":0,"cache_write_tokens_details":{"ttl_30m_tokens":3000,"ttl_12h_tokens":4000}}}`, 0, 0, 0, 0, false},
		{"bridged details", `{"prompt_tokens_details":{"cache_write_tokens":7000,"cache_write_tokens_details":{"ttl_30m_tokens":3000,"ttl_12h_tokens":4000}}}`, 7000, 7000, 3000, 4000, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			total, short, long, ok := CacheWriteBreakdownFromUsage(gjson.Parse(tt.body), tt.total)
			require.Equal(t, tt.wantTotal, total)
			require.Equal(t, tt.short, short)
			require.Equal(t, tt.long, long)
			require.Equal(t, tt.ok, ok)
		})
	}
}

func TestCacheWriteBreakdownFromUsageExplicitZeroPresence(t *testing.T) {
	for _, tt := range []struct {
		body string
		want bool
	}{
		{`{}`, false},
		{`{"input_tokens_details":{"cache_write_tokens":0}}`, true},
		{`{"cache_creation_input_tokens":0}`, true},
		{`{"input_tokens_details":{"cache_write_tokens":null}}`, false},
		{`{"input_tokens_details":{"cache_write_tokens":-1}}`, false},
		{`{"input_tokens_details":{"cache_write_tokens":"0"}}`, false},
	} {
		require.Equal(t, tt.want, CacheWriteAggregateExplicitZero(gjson.Parse(tt.body), 0), tt.body)
	}
}

func TestCacheWriteAggregateExplicitZeroAliasesAndNumbers(t *testing.T) {
	aliases := []string{
		`{"input_tokens_details":{"cache_write_tokens":%s}}`,
		`{"prompt_tokens_details":{"cache_write_tokens":%s}}`,
		`{"input_tokens_details":{"cache_creation_tokens":%s}}`,
		`{"prompt_tokens_details":{"cache_creation_tokens":%s}}`,
		`{"cache_write_tokens":%s}`,
		`{"cache_creation_input_tokens":%s}`,
		`{"cache_write_input_tokens":%s}`,
		`{"cache_creation_tokens":%s}`,
	}
	values := []struct {
		raw  string
		want bool
	}{
		{"0", true}, {"-0", true}, {"0.0", true}, {"-0.000e+999", true}, {"0e-999", true},
		{"1e-999", false}, {"-1e-999", false}, {"0.0001e-999", false}, {"1", false},
		{"-1", false}, {"null", false}, {`"0"`, false}, {"false", false},
	}
	for alias, template := range aliases {
		for _, value := range values {
			t.Run(fmt.Sprintf("alias-%d/%s", alias, value.raw), func(t *testing.T) {
				body := fmt.Sprintf(template, value.raw)
				require.Equal(t, value.want, CacheWriteAggregateExplicitZero(gjson.Parse(body), 0), body)
			})
		}
	}
}

func TestCacheWriteAggregateExplicitZeroPrecedence(t *testing.T) {
	for _, tc := range []struct {
		name  string
		body  string
		total int
		want  bool
	}{
		{"absent", `{}`, 0, false},
		{"nested zero takes precedence", `{"input_tokens_details":{"cache_creation_tokens":0},"cache_write_tokens":7}`, 0, true},
		{"positive aggregate wins over top zero", `{"cache_write_tokens":0,"cache_creation_input_tokens":7}`, 7, false},
		{"invalid higher nested field", `{"input_tokens_details":{"cache_creation_tokens":null},"cache_write_tokens":0}`, 0, false},
		{"invalid higher top field", `{"cache_write_tokens":"0","cache_creation_input_tokens":0}`, 0, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			require.Equal(t, tc.want, CacheWriteAggregateExplicitZero(gjson.Parse(tc.body), tc.total))
		})
	}
}
