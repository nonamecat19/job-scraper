// Package strutil holds the small string conversions the sources share —
// mostly the pointer-or-nil dance the NormalizedJob optional fields require.
package strutil

func Ptr[T any](v T) *T { return &v }

// FirstNonEmpty returns the first non-empty string, or "" when there is none.
// Sources use it to fall back through several candidate fields, since sites
// routinely populate only one of two places a value could live.
func FirstNonEmpty(candidates ...string) string {
	for _, c := range candidates {
		if c != "" {
			return c
		}
	}
	return ""
}

func NilIfEmpty(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}

func StringOr(v any, fallback string) string {
	if s, ok := v.(string); ok && s != "" {
		return s
	}
	return fallback
}

func Truncate(s string, n int) string {
	if n <= 0 {
		return ""
	}
	runes := []rune(s)
	if len(runes) <= n {
		return s
	}
	return string(runes[:n])
}
