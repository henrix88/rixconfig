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

	SetVariableDefaults(VarDefaultsFromYAML([]byte("---\nmy_secret_value: veryverysecretkey\nunknownkey: hi there\nint_var: 42\n")))
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
	SetVariableDefaults(VarDefaultsFromYAMLFile(tmp.Name()))
	exec("defaults from YAML file", [][2]interface{}{
		{&cfg.IntVar, int64(42)},
		{&cfg.MySecretValue, "veryverysecretkey"},
		{&cfg.MyUsername, "luzifer"},
		{&cfg.SomeVar, ""},
	})

	SetVariableDefaults(VarDefaultsFromYAML([]byte("---\nmy_secret_value = veryverysecretkey\nunknownkey = hi there\nint_var = 42\n")))
	exec("defaults from invalid YAML data", [][2]interface{}{
		{&cfg.IntVar, int64(23)},
		{&cfg.MySecretValue, "secret"},
		{&cfg.MyUsername, "luzifer"},
		{&cfg.SomeVar, ""},
	})

	SetVariableDefaults(VarDefaultsFromYAMLFile("/tmp/this_file_should_not_exist_146e26723r"))
	exec("defaults from non-existing YAML file", [][2]interface{}{
		{&cfg.IntVar, int64(23)},
		{&cfg.MySecretValue, "secret"},
		{&cfg.MyUsername, "luzifer"},
		{&cfg.SomeVar, ""},
	})
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
	flat := VarDefaultsFromYAML([]byte(yamlData))
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
	flat := VarDefaultsFromYAML(empty)
	assert.Empty(t, flat)
}

func TestVarDefaultsFromYAMLFile_Errors(t *testing.T) {
	// Non-existent file
	flat := VarDefaultsFromYAMLFile("/tmp/definitely_not_existing_file_1234567890.yaml")
	assert.Empty(t, flat)
}

func TestFlattenYAMLMap_InterfaceKeys(t *testing.T) {
	// Simulate map[interface{}]interface{} input
	in := map[string]interface{}{
		"foo": map[interface{}]interface{}{
			"bar": 42,
		},
	}
	out := map[string]string{}
	flattenYAMLMap("", in, out)
	assert.Equal(t, "42", out["foo.bar"])
}
