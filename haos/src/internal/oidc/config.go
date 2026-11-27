package oidc

import (
	"bytes"
	_ "embed"
	"encoding/json"
	"errors"
	"io"
	"os"
	"slices"
	"strings"

	"go.yaml.in/yaml/v3"
)

const (
	configDir       = ".haos-lpk-oidc"
	providerConfig  = configDir + "/provider.yaml"
	privateConfig   = configDir + "/secrets.yaml"
	bootstrapMarker = ".haos-lpk-oidc-initialized"
	mainConfig      = "configuration.yaml"
	managedHeader   = "# Managed by LazyCat HAOS OIDC\n"
	includeMarker   = "# Managed by LazyCat HAOS OIDC"
)

//go:embed provider.yaml
var defaultProvider []byte

type settings struct {
	ClientID     string `json:"client_id"`
	ClientSecret string `json:"client_secret"`
	DiscoveryURL string `json:"discovery_url"`
}

func privateYAML(data []byte) ([]byte, error) {
	var value settings
	if json.Unmarshal(data, &value) != nil || value.ClientID == "" || value.ClientSecret == "" ||
		value.DiscoveryURL == "" {
		return nil, errors.New("incomplete shared OIDC settings")
	}
	// JSON scalars are YAML-compatible, including quotes, newlines and colons.
	body, err := json.Marshal(value)
	if err != nil {
		return nil, err
	}
	return append(append([]byte(managedHeader), body...), '\n'), nil
}

// Wait for Core itself to finish onboarding. Its owner record and UI entries
// remain exclusively under Core's control.
func readStore[T any](root *os.Root, name string) (*T, error) {
	path := ".storage/" + name
	b, err := root.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var store struct{ Data *T }
	if json.Unmarshal(b, &store) != nil || store.Data == nil {
		return nil, errors.New("invalid Home Assistant storage file: " + path)
	}
	return store.Data, nil
}

func hasUIProvider(root *os.Root) (bool, error) {
	entries, err := readStore[struct{ Entries []struct{ Domain string } }](root, "core.config_entries")
	if err != nil {
		return false, err
	}
	if entries != nil {
		if entries.Entries == nil {
			return false, errors.New("invalid Home Assistant config entries")
		}
		for _, entry := range entries.Entries {
			if entry.Domain == "auth_oidc" {
				return true, nil
			}
		}
	}
	return false, nil
}

func onboardingComplete(root *os.Root) (bool, error) {
	steps, err := readStore[struct{ Done []string }](root, "onboarding")
	if err != nil || steps == nil {
		return false, err
	}
	for _, step := range []string{"user", "core_config", "integration", "analytics"} {
		if !slices.Contains(steps.Done, step) {
			return false, nil
		}
	}
	type user struct {
		Owner  bool `json:"is_owner"`
		Active bool `json:"is_active"`
	}
	auth, err := readStore[struct{ Users []user }](root, "auth")
	if err != nil || auth == nil {
		return false, err
	}
	for _, u := range auth.Users {
		if u.Owner && u.Active {
			return true, nil
		}
	}
	return false, nil
}

func domain(key string) string {
	name, _, _ := strings.Cut(key, " ")
	return name
}

// Return the exact original bytes for an existing managed include. For a new
// one, insert ahead of the first mapping key so comments, includes, CRLF and
// the optional end-of-document marker survive unchanged.
func withInclude(data []byte) (updated []byte, home *yaml.Node, err error) {
	if len(bytes.TrimSpace(data)) == 0 {
		return nil, nil, nil
	}
	var doc, extra yaml.Node
	decoder := yaml.NewDecoder(bytes.NewReader(data))
	if decoder.Decode(&doc) != nil || len(doc.Content) != 1 || decoder.Decode(&extra) != io.EOF {
		return nil, nil, errors.New("cannot safely extend this Home Assistant YAML configuration")
	}
	mapping := doc.Content[0]
	if mapping.Kind != yaml.MappingNode || mapping.Tag != "!!map" {
		return nil, nil, errors.New("cannot safely extend this Home Assistant YAML configuration")
	}
	managed := false
	for i := 0; i < len(mapping.Content); i += 2 {
		key, value := mapping.Content[i], mapping.Content[i+1]
		if key.Tag != "!!str" {
			return nil, nil, errors.New("cannot safely extend this Home Assistant YAML configuration")
		}
		switch domain(key.Value) {
		case "auth_oidc":
			if managed || key.Value != "auth_oidc" || value.Tag != "!include" || value.Value != providerConfig {
				return nil, nil, nil
			}
			managed = true
		case "homeassistant":
			if home != nil || key.Value != "homeassistant" {
				return nil, nil, errors.New("cannot safely extend this Home Assistant YAML configuration")
			}
			home = value
		}
	}
	if managed {
		return data, home, nil
	}
	if len(mapping.Content) == 0 || mapping.Style&yaml.FlowStyle != 0 {
		return nil, nil, errors.New("cannot safely extend this Home Assistant YAML configuration")
	}
	key := mapping.Content[0]
	lines := bytes.SplitAfter(data, []byte("\n"))
	line := key.Line - 1
	if line < 0 || line >= len(lines) || key.Column < 1 {
		return nil, nil, errors.New("cannot safely extend this Home Assistant YAML configuration")
	}
	indent := strings.Repeat(" ", key.Column-1)
	if !bytes.HasPrefix(lines[line], []byte(indent)) {
		return nil, nil, errors.New("cannot safely extend this Home Assistant YAML configuration")
	}
	eol := "\n"
	if bytes.HasSuffix(lines[line], []byte("\r\n")) {
		eol = "\r\n"
	}
	entry := []byte(indent + "auth_oidc: !include " + providerConfig + " " + includeMarker + eol)
	before := bytes.Join(lines[:line], nil)
	after := bytes.Join(lines[line:], nil)
	result := make([]byte, 0, len(data)+len(entry))
	result = append(result, before...)
	result = append(result, entry...)
	result = append(result, after...)
	// Reject uncommon layouts, e.g. a mapping on the document-start line,
	// unless the inserted line parses as our managed top-level entry.
	var verify yaml.Node
	if yaml.Unmarshal(result, &verify) != nil || len(verify.Content) != 1 || len(verify.Content[0].Content) != len(mapping.Content)+2 {
		return nil, nil, errors.New("cannot safely extend this Home Assistant YAML configuration")
	}
	return result, home, nil
}
