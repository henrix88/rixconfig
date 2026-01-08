package rconfig

import (
	"fmt"
	"os"
	"github.com/goccy/go-yaml"
)

// VarDefaultsFromYAMLFile reads contents of a file and calls VarDefaultsFromYAML
func VarDefaultsFromYAMLFile(filename string) map[string]string {
	data, err := os.ReadFile(filename) //#nosec:G304 // Loading file from var is intended
	if err != nil {
		return make(map[string]string)
	}
	return VarDefaultsFromYAML(data)
}

// VarDefaultsFromYAML creates a vardefaults map from YAML raw data, supporting nested YAML by flattening keys.
func VarDefaultsFromYAML(in []byte) map[string]string {
	var raw map[string]interface{}
	if err := yaml.Unmarshal(in, &raw); err != nil {
		return make(map[string]string)
	}
	flat := make(map[string]string)
	flattenYAMLMap("", raw, flat)
	return flat
}

// flattenYAMLMap recursively flattens a nested map into dot-separated keys.
func flattenYAMLMap(prefix string, in map[string]interface{}, out map[string]string) {
	for k, v := range in {
		key := k
		if prefix != "" {
			key = prefix + "." + k
		}
		switch val := v.(type) {
		case map[string]interface{}:
			flattenYAMLMap(key, val, out)
		case map[interface{}]interface{}:
			// Handle maps with interface{} keys (older YAML libs)
			m2 := make(map[string]interface{})
			for mk, mv := range val {
				m2[fmt.Sprintf("%v", mk)] = mv
			}
			flattenYAMLMap(key, m2, out)
		default:
			out[key] = fmt.Sprintf("%v", val)
		}
	}
}
