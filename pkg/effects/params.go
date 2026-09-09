package effects

import "math"

// getFloat reads a float64 parameter from a YAML-derived params map. YAML
// numbers decode as int or float64, so both are accepted. Missing or
// non-numeric values fall back to def.
func getFloat(params map[string]any, key string, def float64) float64 {
	v, ok := params[key]
	if !ok {
		return def
	}
	switch n := v.(type) {
	case float64:
		return n
	case int:
		return float64(n)
	case int64:
		return float64(n)
	}
	return def
}

// getInt reads an int parameter. float64 values are accepted only when they
// are integral; anything else falls back to def.
func getInt(params map[string]any, key string, def int) int {
	v, ok := params[key]
	if !ok {
		return def
	}
	switch n := v.(type) {
	case int:
		return n
	case int64:
		return int(n)
	case float64:
		if n == math.Trunc(n) {
			return int(n)
		}
	}
	return def
}

// getBool reads a bool parameter.
func getBool(params map[string]any, key string, def bool) bool {
	v, ok := params[key]
	if !ok {
		return def
	}
	if b, ok := v.(bool); ok {
		return b
	}
	return def
}

// getString reads a string parameter.
func getString(params map[string]any, key string, def string) string {
	v, ok := params[key]
	if !ok {
		return def
	}
	if s, ok := v.(string); ok {
		return s
	}
	return def
}
