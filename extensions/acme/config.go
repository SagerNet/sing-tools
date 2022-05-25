package acme

import (
	"encoding/json"
	"os"

	"github.com/go-acme/lego/v4/challenge"
	"github.com/sagernet/sing-tools/extensions/acme/cloudflare"
	E "github.com/sagernet/sing/common/exceptions"
)

type Settings struct {
	Enabled       bool     `json:"enabled"`
	DataDirectory string   `json:"data_directory"`
	Email         string   `json:"email"`
	DNSProvider   string   `json:"dns_provider"`
	DNSEnv        *JSONMap `json:"dns_env"`
}

type JSONMap struct {
	json.RawMessage
	Data map[string]any
}

func (m *JSONMap) MarshalJSON() ([]byte, error) {
	if m == nil {
		return []byte("null"), nil
	}
	return json.Marshal(m.Data)
}

// UnmarshalJSON sets *m to a copy of data.
func (m *JSONMap) UnmarshalJSON(data []byte) error {
	if m == nil {
		return E.New("JSONMap: UnmarshalJSON on nil pointer")
	}
	if m.Data == nil {
		m.Data = make(map[string]any)
	}
	return json.Unmarshal(data, &m.Data)
}

func (s *Settings) SetupEnvironment() error {
	for envName, envValue := range s.DNSEnv.Data {
		err := os.Setenv(envName, envValue.(string))
		if err != nil {
			return err
		}
	}
	return nil
}

func NewDNSChallengeProviderByName(name string) (challenge.Provider, error) {
	switch name {
	case "cloudflare":
		return cloudflare.NewDNSProvider()
	}
	// return dns.NewDNSChallengeProviderByName(name)
	return nil, E.New("unsupported dns provider ", name)
}
