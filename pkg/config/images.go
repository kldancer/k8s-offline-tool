package config

import (
	_ "embed"
	"fmt"

	"gopkg.in/yaml.v3"
)

//go:embed images.yaml
var imagesYAML []byte

func ImagesByGroup() (map[string][]string, error) {
	groups := make(map[string][]string)
	if err := yaml.Unmarshal(imagesYAML, &groups); err != nil {
		return nil, fmt.Errorf("parse images.yaml failed: %w", err)
	}
	return groups, nil
}
