package rconfig

import (
	"os"
	"reflect"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestVardefaultParsing(t *testing.T) {
	type test struct {
		MySecretValue string `default:"secret" env:"foo" vardefault:"my_secret_value"`
		MyUsername    string `default:"luzifer" vardefault:"username"`
		SomeVar       string `flag:"var" description:"some variable"`
		IntVar        int64  `vardefault:"int_var" default:"23"`
	}

	var (
		cfg         test
		args        = []string{}
		err         error
		vardefaults = map[string]string{
			"my_secret_value": "veryverysecretkey",
			"unkownkey":       "hi there",
			"int_var":         "42",
		}
	)

	exec := func(desc string, tests [][2]interface{}) {
		require.NoError(t, parse(&cfg, args))

		for _, test := range tests {
			assert.Equal(t, test[1], reflect.ValueOf(test[0]).Elem().Interface(), desc)
		}
	}

	SetVariableDefaults(vardefaults)
	exec("manually provided variables", [][2]interface{}{
		{&cfg.IntVar, int64(42)},
		{&cfg.MySecretValue, "veryverysecretkey"},
		{&cfg.MyUsername, "luzifer"},
		{&cfg.SomeVar, ""},
	})

	defaults, err := VarDefaultsFromYAML([]byte("---\nmy_secret_value: veryverysecretkey\nunknownkey: hi there\nint_var: 42\n"))
	require.NoError(t, err)
	SetVariableDefaults(defaults)

	exec("defaults from YAML data", [][2]interface{}{
		{&cfg.IntVar, int64(42)},
		{&cfg.MySecretValue, "veryverysecretkey"},
		{&cfg.MyUsername, "luzifer"},
		{&cfg.SomeVar, ""},
	})

	tmp, _ := os.CreateTemp("", "")
	t.Cleanup(func() {
		tmp.Close()           //nolint:errcheck,gosec,revive // Just cleanup, will be closed automatically
		os.Remove(tmp.Name()) //nolint:errcheck,gosec,revive // Just cleanup of tmp-file
	})
	yamlData := "---\nmy_secret_value: veryverysecretkey\nunknownkey: hi there\nint_var: 42\n"
	_, err = tmp.WriteString(yamlData)
	require.NoError(t, err)
	
	defaultsFromFile, err := VarDefaultsFromYAMLFile(tmp.Name())
	require.NoError(t, err)
	SetVariableDefaults(defaultsFromFile)

	exec("defaults from YAML file", [][2]interface{}{
		{&cfg.IntVar, int64(42)},
		{&cfg.MySecretValue, "veryverysecretkey"},
		{&cfg.MyUsername, "luzifer"},
		{&cfg.SomeVar, ""},
	})

	// Test invalid YAML data
	_, err = VarDefaultsFromYAML([]byte("---\nmy_secret_value = veryverysecretkey\nunknownkey = hi there\nint_var = 42\n"))
	assert.Error(t, err, "should fail on invalid YAML")

	// Test non-existing YAML file
	_, err = VarDefaultsFromYAMLFile("/tmp/this_file_should_not_exist_146e26723r")
	assert.Error(t, err, "should fail on missing file")
}

func TestVarDefaultsFromYAML_Nested(t *testing.T) {
	yamlData := `
config:
  rabbitmq:
    host: test-host
    port: 1234
    vhost: /testvhost
    client_id: test-client
  logging:
    level: debug
    dir: /tmp/test-logs
    format: text
    add_source: false
`
	flat, err := VarDefaultsFromYAML([]byte(yamlData))
	require.NoError(t, err)

	assert.Equal(t, "test-host", flat["config.rabbitmq.host"])
	assert.Equal(t, "1234", flat["config.rabbitmq.port"])
	assert.Equal(t, "/testvhost", flat["config.rabbitmq.vhost"])
	assert.Equal(t, "test-client", flat["config.rabbitmq.client_id"])
	assert.Equal(t, "debug", flat["config.logging.level"])
	assert.Equal(t, "/tmp/test-logs", flat["config.logging.dir"])
	assert.Equal(t, "text", flat["config.logging.format"])
	assert.Equal(t, "false", flat["config.logging.add_source"])
}

func TestVarDefaultsFromYAML_Errors(t *testing.T) {
	// Empty input should return empty map
	empty := []byte("")
	flat, err := VarDefaultsFromYAML(empty)
	assert.NoError(t, err)
	assert.Empty(t, flat)
}

func TestVarDefaultsFromYAMLFile_Errors(t *testing.T) {
	// Non-existent file
	_, err := VarDefaultsFromYAMLFile("/tmp/definitely_not_existing_file_1234567890.yaml")
	assert.Error(t, err)
}

func TestVarDefaultsFromYAML_LowerCase(t *testing.T) {
	yamlData := `
Config:
  RabbitMQ:
    Host: Test-Host
`
	// Without lowercase option
	flat, err := VarDefaultsFromYAML([]byte(yamlData))
	require.NoError(t, err)
	assert.Equal(t, "Test-Host", flat["Config.RabbitMQ.Host"])

	// With lowercase option
	flat, err = VarDefaultsFromYAML([]byte(yamlData), WithKeyToLower())
	require.NoError(t, err)
	assert.Equal(t, "Test-Host", flat["config.rabbitmq.host"])
}

func TestFlattenYAMLMap_InterfaceKeys(t *testing.T) {
	// Simulate map[interface{}]interface{} input
	// Since flattenYAMLMap is private, we can't call it directly easily unless we export it
	// or use a helper that calls it.
	// But VarDefaultsFromYAML calls it.
	// However, VarDefaultsFromYAML now unmarshals to map[string]interface{}.
	// We can rely on Unmarshal behavior or trust internal logic.
	// But `flattenYAMLMap` handles map[interface{}]interface{} case just in case.
	// We can manually trigger it if we can construct such input to VarDefaultsFromYAML?
	// Usually strict parsers produce string keys.
	// We can skip this test or reflect it if really needed, but it was testing internal logic.
	// Since we redefined flattenYAMLMap in the same file, we can test it if we can access it within `package rconfig`.
	// Yes, unit tests are in `package rconfig`.
	
	in := map[string]interface{}{
		"foo": map[interface{}]interface{}{
			"bar": 42,
		},
	}
	out := map[string]string{}
	opts := &YAMLOptions{}
	flattenYAMLMap("", in, out, opts)
	assert.Equal(t, "42", out["foo.bar"])
}

func TestVarDefaultsFromYAML_SliceRoot(t *testing.T) {
	// YAML root is a list
	yamlData := `
- item1
- item2
`
	_, err := VarDefaultsFromYAML([]byte(yamlData))
	assert.Error(t, err)
	// Check that it fails with parsing error
	assert.Contains(t, err.Error(), "parsing yaml")
}
