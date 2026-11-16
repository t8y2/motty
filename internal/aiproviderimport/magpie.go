package aiproviderimport

import (
	"encoding/json"
	"fmt"
	"io"
	"sort"
	"strings"
)

// magpieGatewayURL is the loopback endpoint magpie documents for Anthropic
// clients. The key "magpie" is the gateway's documented credential, not a
// secret copied from a provider.
const (
	magpieGatewayURL = "http://127.0.0.1:3425"
	magpieGatewayKey = "magpie"
)

type magpieProvider struct {
	ID         textValue         `json:"id"`
	Name       textValue         `json:"name"`
	Key        textValue         `json:"key"`
	Chat       textValue         `json:"chat"`
	Anthropic  textValue         `json:"anthropic"`
	Responses  textValue         `json:"responses"`
	Gemini     textValue         `json:"gemini"`
	Models     []json.RawMessage `json:"models"`
	AuthHeader textValue         `json:"authHeader"`
	Off        json.RawMessage   `json:"off"`

	// cleaned holds the bounded model names computed once after decoding. The
	// gateway and the upstream candidates both read it; parsing the raw models
	// twice is the difference between a bounded import and a large transient
	// allocation on a hostile file.
	cleaned []string
}

// textValue accepts a JSON string or null and ignores other scalar types, so a
// single malformed field does not make the whole source unreadable.
type textValue string

func (v *textValue) UnmarshalJSON(raw []byte) error {
	var text string
	if json.Unmarshal(raw, &text) == nil {
		*v = textValue(text)
		return nil
	}
	*v = ""
	return nil
}

func (v textValue) String() string { return string(v) }

// ParseMagpieFile reads providers.json from disk.
func ParseMagpieFile(path string) ([]Candidate, error) {
	data, err := readFileBounded(path, MaxJSONBytes)
	if err != nil {
		return nil, err
	}
	return ParseMagpieBytes(data)
}

// ParseMagpie reads providers.json.
func ParseMagpie(r io.Reader) ([]Candidate, error) {
	data, err := readBounded(r, MaxJSONBytes)
	if err != nil {
		return nil, err
	}
	return ParseMagpieBytes(data)
}

// ParseMagpieBytes parses an already bounded providers.json document.
func ParseMagpieBytes(data []byte) ([]Candidate, error) {
	var file struct {
		Providers []json.RawMessage `json:"providers"`
	}
	if err := json.Unmarshal(data, &file); err != nil {
		return nil, fmt.Errorf("%w: %v", ErrMalformed, err)
	}
	if file.Providers == nil {
		return nil, fmt.Errorf("%w: no providers array", ErrMalformed)
	}
	if len(file.Providers) > MaxProviders {
		return nil, fmt.Errorf("%w: %d providers", ErrTooLarge, len(file.Providers))
	}

	var out []Candidate
	gateway := Candidate{
		Source: SourceMagpieGateway, Name: "magpie", Kind: KindAnthropic,
		BaseURL: magpieGatewayURL, AuthStyle: AuthStyleAPIKey, Key: magpieGatewayKey,
		Note: NoteMagpieGateway,
	}
	providers := make([]magpieProvider, 0, len(file.Providers))
	gatewaySeen := make(map[string]bool, MaxModels)
	for _, raw := range file.Providers {
		var provider magpieProvider
		if err := json.Unmarshal(raw, &provider); err != nil {
			object, _ := jsonObject(raw)
			name := CleanName(firstNonEmpty(stringField(object, "name"), stringField(object, "id")))
			out = append(out, Candidate{
				Source: SourceMagpieUpstream, Name: name, SkipReason: ReasonMalformed,
			})
			continue
		}
		provider.cleaned = cleanModelMessages(provider.Models)
		provider.Models = nil
		providers = append(providers, provider)
		if isOff(provider.Off) {
			continue
		}
		id := CleanName(provider.ID.String())
		if id == "" || len(gateway.Models) >= MaxModels {
			continue
		}
		// Cap while appending. Appending every provider's list first and
		// trimming afterwards lets a small file allocate one entry per model
		// across every provider before the bound is applied.
		for _, model := range provider.cleaned {
			if len(gateway.Models) >= MaxModels {
				break
			}
			name := id + "/" + model
			if gatewaySeen[name] {
				continue
			}
			gatewaySeen[name] = true
			gateway.Models = append(gateway.Models, name)
		}
	}
	sort.Strings(gateway.Models)
	out = append([]Candidate{gateway}, out...)

	for _, provider := range providers {
		out = append(out, magpieCandidates(provider)...)
	}
	return out, nil
}

func isOff(raw json.RawMessage) bool {
	var value bool
	if json.Unmarshal(raw, &value) == nil {
		return value
	}
	var text string
	if json.Unmarshal(raw, &text) == nil {
		return strings.EqualFold(strings.TrimSpace(text), "true")
	}
	return false
}

func magpieCandidates(provider magpieProvider) []Candidate {
	name := CleanName(provider.Name.String())
	if name == "" {
		name = CleanName(provider.ID.String())
	}
	if name == "" {
		name = "magpie"
	}
	models := provider.cleaned
	if isOff(provider.Off) {
		return []Candidate{{Source: SourceMagpieUpstream, Name: name, Models: models, SkipReason: ReasonDisabled}}
	}
	anthropic := strings.TrimSpace(provider.Anthropic.String())
	chat := strings.TrimSpace(provider.Chat.String())
	hasAnthropic := validEndpoint(anthropic)
	hasChat := validEndpoint(chat)
	if !hasAnthropic && !hasChat {
		reason := ReasonNoEndpoint
		if strings.TrimSpace(provider.Responses.String()) != "" || strings.TrimSpace(provider.Gemini.String()) != "" {
			reason = ReasonUnsupportedProtocol
		}
		return []Candidate{{Source: SourceMagpieUpstream, Name: name, Models: models, SkipReason: reason}}
	}

	header := lowerTrim(provider.AuthHeader.String())
	bearer := header == "bearer" || header == "authorization" || strings.Contains(header, "bearer")
	dual := hasAnthropic && hasChat
	var out []Candidate
	if hasAnthropic {
		authStyle := AuthStyleAPIKey
		if bearer {
			authStyle = AuthStyleBearer
		}
		candidateName := name
		if dual {
			candidateName += " (Anthropic)"
		}
		out = append(out, Candidate{
			Source: SourceMagpieUpstream, Name: candidateName, Kind: KindAnthropic,
			BaseURL: anthropic, AuthStyle: authStyle, Key: strings.TrimSpace(provider.Key.String()), Models: models,
		})
	}
	if hasChat {
		candidateName := name
		if dual {
			candidateName += " (OpenAI)"
		}
		candidate := Candidate{
			Source: SourceMagpieUpstream, Name: candidateName, Kind: KindOpenAI,
			BaseURL: chat, Key: strings.TrimSpace(provider.Key.String()), Models: models,
		}
		// OpenAI-compatible requests always use bearer authentication. A chat
		// endpoint that requires x-api-key cannot be represented faithfully.
		if header == "x-api-key" {
			candidate.Key = ""
			candidate.SkipReason = ReasonUnsupportedAuth
		}
		out = append(out, candidate)
	}
	return out
}
