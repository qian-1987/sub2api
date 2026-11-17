package openai

import (
	"encoding/json"
	"math"
	"strings"

	"github.com/tidwall/gjson"
)

// CacheWriteTokensDetails is the Responses API's non-overlapping cache-write
// breakdown. Pointers distinguish an explicitly reported zero from a missing
// bucket; a request's TTL is never used to infer missing usage.
type CacheWriteTokensDetails struct {
	TTL30mTokens *int `json:"ttl_30m_tokens"`
	TTL12hTokens *int `json:"ttl_12h_tokens"`
}

// Counts accepts a complete, non-negative breakdown only when it agrees with
// the aggregate. Invalid/partial details retain the legacy aggregate behavior.
func (d *CacheWriteTokensDetails) Counts(total int) (short, long int, ok bool) {
	if d == nil || d.TTL30mTokens == nil || d.TTL12hTokens == nil {
		return 0, 0, false
	}
	short, long = *d.TTL30mTokens, *d.TTL12hTokens
	if short < 0 || long < 0 || short > math.MaxInt-long || short+long != total {
		return 0, 0, false
	}
	return short, long, true
}

// CacheWriteAggregateExplicitZero distinguishes an authoritative zero from an
// omitted/invalid aggregate when merging cumulative streaming snapshots.
func CacheWriteAggregateExplicitZero(usage gjson.Result, total int) bool {
	if total != 0 {
		return false
	}
	for _, path := range []string{
		"input_tokens_details.cache_write_tokens", "prompt_tokens_details.cache_write_tokens",
		"input_tokens_details.cache_creation_tokens", "prompt_tokens_details.cache_creation_tokens",
		"cache_write_tokens", "cache_creation_input_tokens", "cache_write_input_tokens", "cache_creation_tokens",
	} {
		if value := usage.Get(path); value.Exists() {
			return cacheWriteNumberIsExactlyZero(value)
		}
	}
	return false
}

// Check the JSON mantissa rather than a float conversion: values such as
// 1e-999 underflow to float64 zero but are not mathematically zero.
func cacheWriteNumberIsExactlyZero(value gjson.Result) bool {
	if value.Type != gjson.Number || !json.Valid([]byte(value.Raw)) {
		return false
	}
	mantissa := value.Raw
	if i := strings.IndexAny(mantissa, "eE"); i >= 0 {
		mantissa = mantissa[:i]
	}
	for i := 0; i < len(mantissa); i++ {
		switch mantissa[i] {
		case '0', '-', '.':
		default:
			return false
		}
	}
	return true
}

// CacheWriteBreakdownFromUsage parses details actually returned by an upstream.
// prompt_tokens_details is accepted as a compatibility extension when a Responses
// response is bridged to Chat Completions; no such details are fabricated.
func CacheWriteBreakdownFromUsage(usage gjson.Result, total int) (aggregate, short, long int, ok bool) {
	aggregate = total
	var details gjson.Result
	for _, path := range []string{"input_tokens_details.cache_write_tokens_details", "prompt_tokens_details.cache_write_tokens_details"} {
		if candidate := usage.Get(path); candidate.Exists() {
			details = candidate
			break
		}
	}
	shortValue, longValue := details.Get("ttl_30m_tokens"), details.Get("ttl_12h_tokens")
	if !details.IsObject() || shortValue.Type != gjson.Number || longValue.Type != gjson.Number {
		return aggregate, 0, 0, false
	}
	short, long = int(shortValue.Int()), int(longValue.Int())
	if short < 0 || long < 0 || shortValue.Float() != float64(short) || longValue.Float() != float64(long) || short > math.MaxInt-long {
		return aggregate, 0, 0, false
	}
	hasAggregate := false
	for _, path := range []string{
		"input_tokens_details.cache_write_tokens", "prompt_tokens_details.cache_write_tokens",
		"input_tokens_details.cache_creation_tokens", "prompt_tokens_details.cache_creation_tokens",
		"cache_write_tokens", "cache_creation_input_tokens", "cache_write_input_tokens", "cache_creation_tokens",
	} {
		if usage.Get(path).Exists() {
			hasAggregate = true
			break
		}
	}
	if !hasAggregate {
		// A compatible upstream may omit the aggregate while retaining both
		// complete buckets. Recover it once, rather than count the buckets twice.
		aggregate = short + long
	}
	if short+long != aggregate {
		return aggregate, 0, 0, false
	}
	return aggregate, short, long, true
}
