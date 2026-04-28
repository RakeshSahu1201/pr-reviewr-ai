package llm

import (
	"context"
	"regexp"
	"strings"
)

// ─────────────────────────────────────────────────────────────────────────────
// Sanitizer — strips secrets, PII, and proprietary conventions from code
// before it leaves the process boundary to reach any LLM provider.
// ─────────────────────────────────────────────────────────────────────────────

// redactionRule is a named regexp pattern with its replacement token.
type redactionRule struct {
	name        string
	pattern     *regexp.Regexp
	replacement string
}

// defaultRules covers the most common secret and PII patterns.
var defaultRules = []redactionRule{
	// Generic API key assignments:  key = "sk-..."  token="glpat-..."
	{
		name:        "generic-key-assignment",
		pattern:     regexp.MustCompile(`(?i)(api[_-]?key|secret|token|password|passwd|pwd|auth)\s*[=:]\s*["']?[\w\-\.]{8,}["']?`),
		replacement: `$1 = "[REDACTED]"`,
	},
	// Anthropic / OpenAI key prefixes
	{"anthropic-key", regexp.MustCompile(`sk-ant-[A-Za-z0-9\-_]{20,}`), "[ANTHROPIC_KEY]"},
	{"openai-key", regexp.MustCompile(`sk-[A-Za-z0-9]{20,}`), "[OPENAI_KEY]"},
	// GitLab PAT
	{"gitlab-pat", regexp.MustCompile(`glpat-[A-Za-z0-9\-_]{20,}`), "[GITLAB_PAT]"},
	// GitHub tokens
	{"github-token", regexp.MustCompile(`gh[pousr]_[A-Za-z0-9]{36,}`), "[GITHUB_TOKEN]"},
	// AWS Access Key ID
	{"aws-key-id", regexp.MustCompile(`AKIA[0-9A-Z]{16}`), "[AWS_KEY_ID]"},
	// AWS Secret Access Key (40 chars base62)
	{"aws-secret", regexp.MustCompile(`(?i)aws.{0,20}secret.{0,20}[=:]\s*["']?[A-Za-z0-9\/+=]{40}["']?`), "[AWS_SECRET]"},
	// Private keys / certificates
	{"pem-key", regexp.MustCompile(`-----BEGIN [A-Z ]+KEY-----[\s\S]*?-----END [A-Z ]+KEY-----`), "[PEM_KEY_BLOCK]"},
	// Long hex strings (32+ chars) — likely keys/hashes
	{"hex-secret", regexp.MustCompile(`\b[0-9a-fA-F]{32,}\b`), "[HEX_SECRET]"},
	// Base64 encoded blobs (40+ contiguous base64 chars)
	{"base64-blob", regexp.MustCompile(`[A-Za-z0-9+/]{40,}={0,2}`), "[BASE64_BLOB]"},
	// Email addresses
	{"email", regexp.MustCompile(`[a-zA-Z0-9._%+\-]+@[a-zA-Z0-9.\-]+\.[a-zA-Z]{2,}`), "[EMAIL]"},
	// IPv4 addresses (may be internal infra)
	{"ipv4", regexp.MustCompile(`\b(?:\d{1,3}\.){3}\d{1,3}\b`), "[IP_ADDR]"},
	// Database connection strings
	{"dsn", regexp.MustCompile(`(?i)(postgres|mysql|mongodb|redis):\/\/[^\s'"]+`), "[DSN_REDACTED]"},
	// Bearer / Basic auth values in strings
	{"bearer", regexp.MustCompile(`(?i)(Bearer|Basic)\s+[A-Za-z0-9\-_\.=]{10,}`), "$1 [AUTH_TOKEN]"},
}

// Sanitizer applies redaction rules to code/text before it is sent to an LLM.
type Sanitizer struct {
	rules       []redactionRule
	customTerms []string // company-specific terms/names to redact
}

// NewSanitizer creates a Sanitizer with all default rules pre-loaded.
func NewSanitizer() *Sanitizer {
	return &Sanitizer{rules: append([]redactionRule{}, defaultRules...)}
}

// AddCustomTerms injects proprietary terms (package names, hostnames, etc.)
// that should be replaced with [REDACTED] before sending to any LLM.
//
//	s.AddCustomTerms("MyCompany", "internal-hostname", "ProprietarySDK")
func (s *Sanitizer) AddCustomTerms(terms ...string) *Sanitizer {
	s.customTerms = append(s.customTerms, terms...)
	return s
}

// Sanitize runs all redaction rules against input and returns the cleaned string.
func (s *Sanitizer) Sanitize(input string) string {
	output := input
	for _, rule := range s.rules {
		output = rule.pattern.ReplaceAllString(output, rule.replacement)
	}
	// Company-specific terms — simple case-sensitive replacement.
	for _, term := range s.customTerms {
		output = strings.ReplaceAll(output, term, "[PROPRIETARY]")
	}
	return output
}

// ─────────────────────────────────────────────────────────────────────────────
// SanitizedClient — middleware that wraps any LLMClient
// ─────────────────────────────────────────────────────────────────────────────

// SanitizedClient is a middleware that sanitizes all inputs before
// forwarding them to the wrapped LLMClient. Drop into any layer:
//
//	safe := llm.NewSanitizedClient(registry, sanitizer)
//	pipeline := llm.NewPipeline(safe)   // pipeline is now sanitization-aware
type SanitizedClient struct {
	inner     LLMClient
	sanitizer *Sanitizer
}

// NewSanitizedClient wraps any LLMClient with the sanitization layer.
func NewSanitizedClient(inner LLMClient, s *Sanitizer) *SanitizedClient {
	return &SanitizedClient{inner: inner, sanitizer: s}
}

// Complete sanitizes both prompts then delegates to the wrapped client.
func (sc *SanitizedClient) Complete(ctx context.Context, systemPrompt, userPrompt string) (string, error) {
	return sc.inner.Complete(
		ctx,
		sc.sanitizer.Sanitize(systemPrompt),
		sc.sanitizer.Sanitize(userPrompt),
	)
}

// Analyze sanitizes context and prompt if the inner client is a Provider.
func (sc *SanitizedClient) Analyze(ctx context.Context, codeContext, prompt string) (string, error) {
	clean := sc.sanitizer.Sanitize(codeContext)
	cleanPrompt := sc.sanitizer.Sanitize(prompt)

	// If the inner client supports Analyze() directly, use it.
	if r, ok := sc.inner.(*Registry); ok {
		return r.Analyze(ctx, clean, cleanPrompt)
	}
	// Fallback: compose prompts and use Complete.
	return sc.inner.Complete(ctx,
		"You are a senior software engineer performing code review.",
		"Context:\n"+clean+"\n\nTask:\n"+cleanPrompt,
	)
}
