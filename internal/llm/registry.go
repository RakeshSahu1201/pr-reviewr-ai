package llm

import (
	"context"
	"errors"
	"fmt"
	"log"
	"sync"
	"time"
)

// ErrAllProvidersExhausted is returned when every provider has hit its quota.
var ErrAllProvidersExhausted = errors.New("llm/registry: all providers have hit their quota limits")

// quotaRecord tracks when a provider hit its quota and when it auto-resets.
type quotaRecord struct {
	hitAt      time.Time
	resetAfter time.Duration
}

// Registry holds an ordered list of Provider implementations and automatically
// fails over to the next provider on quota errors.
//
// It implements LLMClient so it can be used as a drop-in for Pipeline:
//
//	pipeline := llm.NewPipeline(registry) // registry satisfies LLMClient
type Registry struct {
	providers    []Provider
	mu           sync.RWMutex
	quotaRecords map[ProviderName]*quotaRecord
}

// NewRegistry creates a Registry. Providers are tried in the supplied order.
func NewRegistry(providers ...Provider) *Registry {
	return &Registry{
		providers:    providers,
		quotaRecords: make(map[ProviderName]*quotaRecord),
	}
}

// Complete implements LLMClient — the existing Pipeline calls this.
// Attempts each registered provider in order, failing over on QuotaError.
func (r *Registry) Complete(ctx context.Context, systemPrompt, userPrompt string) (string, error) {
	return r.attempt(ctx, func(p Provider) (string, error) {
		return p.Complete(ctx, systemPrompt, userPrompt)
	})
}

// Analyze is the user-facing structured analysis entry point.
// Fails over automatically when a provider is quota-exhausted.
func (r *Registry) Analyze(ctx context.Context, codeContext, prompt string) (string, error) {
	return r.attempt(ctx, func(p Provider) (string, error) {
		return p.Analyze(ctx, codeContext, prompt)
	})
}

// Active returns the names of providers that are currently available (not quota-hit).
func (r *Registry) Active() []ProviderName {
	r.mu.RLock()
	defer r.mu.RUnlock()
	var active []ProviderName
	for _, p := range r.providers {
		if !r.isExhausted(p) {
			active = append(active, p.Name())
		}
	}
	return active
}

// attempt is the shared failover loop for Complete and Analyze.
func (r *Registry) attempt(ctx context.Context, fn func(Provider) (string, error)) (string, error) {
	for _, p := range r.providers {
		if r.isExhausted(p) {
			log.Printf("llm/registry: skipping %s (quota exhausted, resets in %s)",
				p.Name(), r.timeToReset(p).Round(time.Minute))
			continue
		}

		result, err := fn(p)
		if err == nil {
			return result, nil
		}

		if IsQuota(err) || p.IsQuotaError(err) {
			r.markExhausted(p, 24*time.Hour) // standard daily reset window
			log.Printf("llm/registry: %s quota hit — failing over to next provider", p.Name())
			continue
		}

		// Non-quota error — surface immediately.
		return "", fmt.Errorf("llm/registry [%s]: %w", p.Name(), err)
	}
	return "", ErrAllProvidersExhausted
}

// isExhausted reports whether p is within its quota-exhausted window.
func (r *Registry) isExhausted(p Provider) bool {
	r.mu.RLock()
	rec, ok := r.quotaRecords[p.Name()]
	r.mu.RUnlock()
	if !ok {
		return false
	}
	return time.Since(rec.hitAt) < rec.resetAfter
}

// timeToReset returns the remaining duration until a provider's quota resets.
func (r *Registry) timeToReset(p Provider) time.Duration {
	r.mu.RLock()
	rec := r.quotaRecords[p.Name()]
	r.mu.RUnlock()
	if rec == nil {
		return 0
	}
	remaining := rec.resetAfter - time.Since(rec.hitAt)
	if remaining < 0 {
		return 0
	}
	return remaining
}

// markExhausted records a quota hit for a provider.
func (r *Registry) markExhausted(p Provider, resetAfter time.Duration) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.quotaRecords[p.Name()] = &quotaRecord{hitAt: time.Now(), resetAfter: resetAfter}
}
