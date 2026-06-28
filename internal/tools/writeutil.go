package tools

import "encoding/json"

// toMap marshals any value to a generic map for spread-style merging,
// mirroring the JS object-spread used in update_table.
func toMap(v any) map[string]any {
	b, err := json.Marshal(v)
	if err != nil {
		return map[string]any{}
	}
	var m map[string]any
	if err := json.Unmarshal(b, &m); err != nil {
		return map[string]any{}
	}
	if m == nil {
		m = map[string]any{}
	}
	return m
}

// cascadeCount extracts an integer count from a cascade ack map. JSON numbers
// decode as float64; any other shape yields 0.
func cascadeCount(cascade any, key string) int {
	m, ok := cascade.(map[string]any)
	if !ok {
		return 0
	}
	switch v := m[key].(type) {
	case float64:
		return int(v)
	case int:
		return v
	default:
		return 0
	}
}
