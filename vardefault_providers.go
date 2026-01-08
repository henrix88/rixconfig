package rconfig

import (
	"fmt"
	"os"
	"strings"

	"github.com/goccy/go-yaml"
)

// YAMLOptions configuration for YAML parsing
type YAMLOptions struct {
	KeyToLower bool
}

// YAMLOption functional option for YAML parsing
type YAMLOption func(*YAMLOptions)

// WithKeyToLower validates keys are lower-cased
func WithKeyToLower() YAMLOption {
	return func(o *YAMLOptions) {
		o.KeyToLower = true
	}
}

// VarDefaultsFromYAMLFile reads contents of a file and calls VarDefaultsFromYAML
func VarDefaultsFromYAMLFile(filename string, opts ...YAMLOption) (map[string]string, error) {
	data, err := os.ReadFile(filename) //#nosec:G304 // Loading file from var is intended
	if err != nil {
		return nil, fmt.Errorf("reading file: %w", err)
	}
	return VarDefaultsFromYAML(data, opts...)
}

// VarDefaultsFromYAML creates a vardefaults map from YAML raw data, supporting nested YAML by flattening keys.
func VarDefaultsFromYAML(in []byte, opts ...YAMLOption) (map[string]string, error) {
	options := &YAMLOptions{}
	for _, opt := range opts {
		opt(options)
	}

	var raw map[string]interface{}
	if err := yaml.Unmarshal(in, &raw); err != nil {
		return nil, fmt.Errorf("parsing yaml: %w", err)
	}

	flat := make(map[string]string)
	flattenYAMLMap("", raw, flat, options)
	return flat, nil
}

// flattenYAMLMap recursively flattens a nested map into dot-separated keys.
func flattenYAMLMap(prefix string, in map[string]interface{}, out map[string]string, opts *YAMLOptions) {
	for k, v := range in {
		key := k
		if opts.KeyToLower {
			key = strings.ToLower(key)
		}

		if prefix != "" {
			key = prefix + "." + key
		}

		switch val := v.(type) {
		case map[string]interface{}:
			flattenYAMLMap(key, val, out, opts)
		case map[interface{}]interface{}:
			// Handle maps with interface{} keys (older YAML libs)
			m2 := make(map[string]interface{})
			for mk, mv := range val {
				m2[fmt.Sprintf("%v", mk)] = mv
			}
			flattenYAMLMap(key, m2, out, opts)
		default:
			out[key] = fmt.Sprintf("%v", val)
		}
	}
}
