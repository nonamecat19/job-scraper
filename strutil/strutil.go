package strutil

func Ptr[T any](v T) *T { return &v }

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
