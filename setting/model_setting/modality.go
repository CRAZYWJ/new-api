package model_setting

import (
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/setting/reasoning"
)

// InputModalities describes which request input types a model accepts.
// A nil flag means the type is not constrained by this entry.
type InputModalities struct {
	Text  *bool `json:"text,omitempty"`
	Image *bool `json:"image,omitempty"`
	Audio *bool `json:"audio,omitempty"`
	File  *bool `json:"file,omitempty"`
}

// InputModalityPolicy is the gateway-level inbound modality filter.
// Unlisted models are not validated.
type InputModalityPolicy struct {
	Enabled bool                       `json:"enabled"`
	Models  map[string]InputModalities `json:"models,omitempty"`
}

func defaultInputModalityPolicy() InputModalityPolicy {
	textOnly := InputModalities{
		Text:  common.GetPointer(true),
		Image: common.GetPointer(false),
	}
	return InputModalityPolicy{
		Enabled: true,
		Models: map[string]InputModalities{
			"deepseek-chat":     textOnly,
			"deepseek-reasoner": textOnly,
			"deepseek-v4-flash": textOnly,
			"deepseek-v4-pro":   textOnly,
		},
	}
}

func GetInputModalityPolicy() InputModalityPolicy {
	return GetGlobalSettings().InputModalityPolicy
}

// LookupInputModalities returns the capability entry for a request model.
// Matching is case-insensitive. Vendor prefixes (provider/model) and effort
// suffixes (-none, -max, -high, ...) are stripped. A configured key also
// matches names that continue with a hyphen, and the longest key wins.
func LookupInputModalities(modelName string) (InputModalities, bool) {
	policy := GetInputModalityPolicy()
	if !policy.Enabled || len(policy.Models) == 0 {
		return InputModalities{}, false
	}

	normalized := make(map[string]InputModalities, len(policy.Models))
	for key, caps := range policy.Models {
		normalizedKey := strings.ToLower(strings.TrimSpace(key))
		if normalizedKey == "" {
			continue
		}
		normalized[normalizedKey] = caps
	}
	if len(normalized) == 0 {
		return InputModalities{}, false
	}

	candidates := modalityModelCandidates(modelName)
	for _, candidate := range candidates {
		if caps, ok := normalized[candidate]; ok {
			return caps, true
		}
	}

	var (
		bestKey  string
		bestCaps InputModalities
		found    bool
	)
	for _, candidate := range candidates {
		for key, caps := range normalized {
			if !strings.HasPrefix(candidate, key) || len(candidate) == len(key) {
				continue
			}
			if candidate[len(key)] != '-' {
				continue
			}
			if !found || len(key) > len(bestKey) {
				bestKey = key
				bestCaps = caps
				found = true
			}
		}
	}
	return bestCaps, found
}

func modalityModelCandidates(modelName string) []string {
	raw := strings.ToLower(strings.TrimSpace(modelName))
	if raw == "" {
		return nil
	}

	seen := map[string]struct{}{}
	var names []string
	add := func(name string) {
		name = strings.TrimSpace(name)
		if name == "" {
			return
		}
		if _, ok := seen[name]; ok {
			return
		}
		seen[name] = struct{}{}
		names = append(names, name)
	}

	add(raw)
	if idx := strings.LastIndex(raw, "/"); idx >= 0 && idx+1 < len(raw) {
		add(raw[idx+1:])
	}

	suffixSets := [][]string{
		reasoning.EffortSuffixes,
		reasoning.DeepSeekV4EffortSuffixes,
		reasoning.OpenAIEffortSuffixes,
	}
	for _, name := range append([]string(nil), names...) {
		for _, suffixes := range suffixSets {
			base, _, ok := reasoning.TrimEffortSuffixWithSuffixes(name, suffixes)
			if ok {
				add(base)
			}
		}
	}
	return names
}
