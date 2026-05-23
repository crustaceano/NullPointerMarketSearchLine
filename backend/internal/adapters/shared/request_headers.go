package shared

import (
	"context"
	"strings"
)

type requestHeadersContextKey struct{}

func WithRequestHeaders(ctx context.Context, headers map[string]string) context.Context {
	if len(headers) == 0 {
		return ctx
	}

	copied := make(map[string]string, len(headers))
	for key, value := range headers {
		key = strings.TrimSpace(key)
		value = strings.TrimSpace(value)
		if key == "" || value == "" {
			continue
		}
		copied[key] = value
	}
	if len(copied) == 0 {
		return ctx
	}

	return context.WithValue(ctx, requestHeadersContextKey{}, copied)
}

func RequestHeaders(ctx context.Context) map[string]string {
	headers, _ := ctx.Value(requestHeadersContextKey{}).(map[string]string)
	return headers
}
