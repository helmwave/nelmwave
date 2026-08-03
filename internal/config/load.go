package config

import (
	"fmt"

	"github.com/helmwave/confijer"
)

// Parse unmarshals already-rendered nelmwave.yml bytes into a Config, applying
// confijer's type-aware defaults. It does not validate; call Validate after.
func Parse(data []byte) (*Config, error) {
	var cfg Config
	if err := confijer.UnmarshalYAML(data, &cfg); err != nil {
		return nil, fmt.Errorf("parse nelmwave config: %w", err)
	}
	return &cfg, nil
}
