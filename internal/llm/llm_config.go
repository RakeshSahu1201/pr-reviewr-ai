package llm

import (
	"fmt"
	"log"
	"strings"

	"pr-reviewer-ai/internal/llm/providers"
)

// ProviderEntry is one entry in the ordered provider list.
type ProviderEntry struct {
	Name   ProviderName
	APIKey string
	Model  string // optional; uses provider default when empty
}

// RegistryConfig holds the ordered provider list and sanitization settings.
type RegistryConfig struct {
	// Providers in failover priority order (index 0 is tried first).
	Providers []ProviderEntry

	// EnableSanitization wraps the final Registry in SanitizedClient.
	EnableSanitization bool

	// CustomRedactTerms lists company-specific strings to strip before sending.
	CustomRedactTerms []string
}

// BuildRegistry constructs and returns a fully wired LLMClient:
//
//	NewGeminiProvider / NewOAICompatProvider → Registry → (SanitizedClient)
//
// The returned client satisfies LLMClient and can substitute any GeminiClient usage.
func BuildRegistry(cfg RegistryConfig) (LLMClient, error) {
	if len(cfg.Providers) == 0 {
		return nil, fmt.Errorf("llm: RegistryConfig must contain at least one provider")
	}

	var impls []Provider
	for _, entry := range cfg.Providers {
		p, err := buildProvider(entry)
		if err != nil {
			log.Printf("llm/config: skipping provider %s: %v", entry.Name, err)
			continue
		}
		impls = append(impls, p)
		log.Printf("llm/config: registered provider %s (model: %s)", p.Name(), p.Model())
	}

	if len(impls) == 0 {
		return nil, fmt.Errorf("llm: no valid providers in config")
	}

	var client LLMClient = NewRegistry(impls...)

	if cfg.EnableSanitization {
		s := NewSanitizer().AddCustomTerms(cfg.CustomRedactTerms...)
		client = NewSanitizedClient(client, s)
		log.Println("llm/config: sanitization middleware enabled")
	}

	return client, nil
}

// buildProvider constructs a single Provider from a ProviderEntry.
func buildProvider(e ProviderEntry) (Provider, error) {
	if strings.TrimSpace(e.APIKey) == "" {
		return nil, fmt.Errorf("API key is empty")
	}
	switch e.Name {
	case ProviderGemini:
		return providers.NewGeminiProvider(e.APIKey, e.Model), nil
	case ProviderGroq:
		cfg := providers.GroqConfig(e.APIKey)
		if e.Model != "" {
			cfg.Model = e.Model
		}
		return providers.NewOAICompatProvider(cfg), nil
	case ProviderDeepSeek:
		cfg := providers.DeepSeekConfig(e.APIKey)
		if e.Model != "" {
			cfg.Model = e.Model
		}
		return providers.NewOAICompatProvider(cfg), nil
	case ProviderMistral:
		cfg := providers.MistralConfig(e.APIKey)
		if e.Model != "" {
			cfg.Model = e.Model
		}
		return providers.NewOAICompatProvider(cfg), nil
	case ProviderCerebras:
		cfg := providers.CerebrasConfig(e.APIKey)
		if e.Model != "" {
			cfg.Model = e.Model
		}
		return providers.NewOAICompatProvider(cfg), nil
	default:
		return nil, fmt.Errorf("unknown provider: %q", e.Name)
	}
}
