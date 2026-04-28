// Package types defines the shared LLM provider types used by both
// the llm package and the llm/providers sub-package.
// It intentionally has NO imports from other project packages to avoid cycles.
package types

import (
	"errors"
	"net/http"
)

// ProviderName is a typed string identifier for an LLM provider.
type ProviderName string

const (
	ProviderGemini   ProviderName = "gemini"
	ProviderGroq     ProviderName = "groq"
	ProviderDeepSeek ProviderName = "deepseek"
	ProviderMistral  ProviderName = "mistral"
	ProviderCerebras ProviderName = "cerebras"
)

// QuotaError is a sentinel returned when a provider hits its daily/rate limit.
// The Registry uses this to trigger automatic failover.
type QuotaError struct {
	Provider ProviderName
	Message  string
}

func (e *QuotaError) Error() string {
	return string(e.Provider) + ": quota exceeded — " + e.Message
}

// IsQuota returns true when err is (or wraps) a *QuotaError.
func IsQuota(err error) bool {
	var q *QuotaError
	return errors.As(err, &q)
}

// IsHTTP429 is a shared helper for providers to check rate-limit status codes.
func IsHTTP429(statusCode int) bool {
	return statusCode == http.StatusTooManyRequests
}
