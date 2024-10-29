// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package expandconverter

import (
	"context"
	"fmt"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
	"go.uber.org/zap/zaptest/observer"

	"go.opentelemetry.io/collector/confmap"
	"go.opentelemetry.io/collector/confmap/confmaptest"
	"go.opentelemetry.io/collector/confmap/internal/envvar"
)

func TestNewExpandConverter(t *testing.T) {
	var testCases = []struct {
		name string // test case name (also file name containing config yaml)
	}{
		{name: "expand-with-no-env.yaml"},
		{name: "expand-with-partial-env.yaml"},
		{name: "expand-with-all-env.yaml"},
	}

	const valueExtra = "some string"
	const valueExtraMapValue = "some map value"
	const valueExtraListMapValue = "some list map value"
	const valueExtraListElement = "some list value"
	t.Setenv("EXTRA", valueExtra)
	t.Setenv("EXTRA_MAP_VALUE_1", valueExtraMapValue+"_1")
	t.Setenv("EXTRA_MAP_VALUE_2", valueExtraMapValue+"_2")
	t.Setenv("EXTRA_LIST_MAP_VALUE_1", valueExtraListMapValue+"_1")
	t.Setenv("EXTRA_LIST_MAP_VALUE_2", valueExtraListMapValue+"_2")
	t.Setenv("EXTRA_LIST_VALUE_1", valueExtraListElement+"_1")
	t.Setenv("EXTRA_LIST_VALUE_2", valueExtraListElement+"_2")

	expectedCfgMap, errExpected := confmaptest.LoadConf(filepath.Join("testdata", "expand-with-no-env.yaml"))
	require.NoError(t, errExpected, "Unable to get expected config")

	for _, tt := range testCases {
		t.Run(tt.name, func(t *testing.T) {
			conf, err := confmaptest.LoadConf(filepath.Join("testdata", tt.name))
			require.NoError(t, err, "Unable to get config")

			// Test that expanded configs are the same with the simple config with no env vars.
			require.NoError(t, createConverter().Convert(context.Background(), conf))
			assert.Equal(t, expectedCfgMap.ToStringMap(), conf.ToStringMap())
		})
	}
}

func TestNewExpandConverter_EscapedMaps(t *testing.T) {
	const receiverExtraMapValue = "some map value"
	t.Setenv("MAP_VALUE", receiverExtraMapValue)

	conf := confmap.NewFromStringMap(
		map[string]any{
			"test_string_map": map[string]any{
				"recv": "$MAP_VALUE",
			},
			"test_interface_map": map[any]any{
				"recv": "$MAP_VALUE",
			}},
	)
	require.NoError(t, createConverter().Convert(context.Background(), conf))

	expectedMap := map[string]any{
		"test_string_map": map[string]any{
			"recv": receiverExtraMapValue,
		},
		"test_interface_map": map[string]any{
			"recv": receiverExtraMapValue,
		}}
	assert.Equal(t, expectedMap, conf.ToStringMap())
}

func TestNewExpandConverter_EscapedEnvVars(t *testing.T) {
	const receiverExtraMapValue = "some map value"
	t.Setenv("MAP_VALUE_2", receiverExtraMapValue)

	// Retrieve the config
	conf, err := confmaptest.LoadConf(filepath.Join("testdata", "expand-escaped-env.yaml"))
	require.NoError(t, err, "Unable to get config")

	expectedMap := map[string]any{
		"test_map": map[string]any{
			// $$ -> escaped $
			"recv.1": "$MAP_VALUE_1",
			// $$$ -> escaped $ + substituted env var
			"recv.2": "$" + receiverExtraMapValue,
			// $$$$ -> two escaped $
			"recv.3": "$$MAP_VALUE_3",
			// escaped $ in the middle
			"recv.4": "some${MAP_VALUE_4}text",
			// $$$$ -> two escaped $
			"recv.5": "${ONE}${TWO}",
			// trailing escaped $
			"recv.6": "text$",
			// escaped $ alone
			"recv.7": "$",
		}}
	require.NoError(t, createConverter().Convert(context.Background(), conf))
	assert.Equal(t, expectedMap, conf.ToStringMap())
}

func TestNewExpandConverterHostPort(t *testing.T) {
	t.Setenv("HOST", "127.0.0.1")
	t.Setenv("PORT", "4317")

	var testCases = []struct {
		name     string
		input    map[string]any
		expected map[string]any
	}{
		{
			name: "brackets",
			input: map[string]any{
				"test": "${HOST}:${PORT}",
			},
			expected: map[string]any{
				"test": "127.0.0.1:4317",
			},
		},
		{
			name: "no brackets",
			input: map[string]any{
				"test": "$HOST:$PORT",
			},
			expected: map[string]any{
				"test": "127.0.0.1:4317",
			},
		},
		{
			name: "mix",
			input: map[string]any{
				"test": "${HOST}:$PORT",
			},
			expected: map[string]any{
				"test": "127.0.0.1:4317",
			},
		},
		{
			name: "reverse mix",
			input: map[string]any{
				"test": "$HOST:${PORT}",
			},
			expected: map[string]any{
				"test": "127.0.0.1:4317",
			},
		},
	}
	for _, tt := range testCases {
		t.Run(tt.name, func(t *testing.T) {
			conf := confmap.NewFromStringMap(tt.input)
			require.NoError(t, createConverter().Convert(context.Background(), conf))
			assert.Equal(t, tt.expected, conf.ToStringMap())
		})
	}
}

func NewTestConverter() (confmap.Converter, *observer.ObservedLogs) {
	core, logs := observer.New(zapcore.InfoLevel)
	conv := converter{loggedDeprecations: make(map[string]struct{}), logger: zap.New(core)}
	return conv, logs
}

func TestDeprecatedWarning(t *testing.T) {
	msgTemplate := `Variable substitution using $VAR will be deprecated in favor of ${VAR} and ${env:VAR}, please update $%s`
	t.Setenv("HOST", "127.0.0.1")
	t.Setenv("PORT", "4317")

	t.Setenv("HOST_NAME", "127.0.0.2")
	t.Setenv("HOSTNAME", "127.0.0.3")

	t.Setenv("BAD!HOST", "127.0.0.2")

	var testCases = []struct {
		name             string
		input            map[string]any
		expectedOutput   map[string]any
		expectedWarnings []string
		expectedError    error
	}{
		{
			name: "no warning",
			input: map[string]any{
				"test": "${HOST}:${PORT}",
			},
			expectedOutput: map[string]any{
				"test": "127.0.0.1:4317",
			},
			expectedWarnings: []string{},
			expectedError:    nil,
		},
		{
			name: "malformed environment variable",
			input: map[string]any{
				"test": "${BAD!HOST}",
			},
			expectedOutput: map[string]any{
				"test": "blah",
			},
			expectedWarnings: []string{},
			expectedError:    fmt.Errorf("environment variable \"BAD!HOST\" has invalid name: must match regex %s", envvar.ValidationRegexp),
		},
		{
			name: "malformed environment variable number",
			input: map[string]any{
				"test": "${2BADHOST}",
			},
			expectedOutput: map[string]any{
				"test": "blah",
			},
			expectedWarnings: []string{},
			expectedError:    fmt.Errorf("environment variable \"2BADHOST\" has invalid name: must match regex %s", envvar.ValidationRegexp),
		},
		{
			name: "malformed environment variable unicode",
			input: map[string]any{
				"test": "${😊BADHOST}",
			},
			expectedOutput: map[string]any{
				"test": "blah",
			},
			expectedWarnings: []string{},
			expectedError:    fmt.Errorf("environment variable \"😊BADHOST\" has invalid name: must match regex %s", envvar.ValidationRegexp),
		},
	}

	for _, tt := range testCases {
		t.Run(tt.name, func(t *testing.T) {
			conf := confmap.NewFromStringMap(tt.input)
			conv, logs := NewTestConverter()
			err := conv.Convert(context.Background(), conf)
			assert.Equal(t, tt.expectedError, err)
			if tt.expectedError == nil {
				assert.Equal(t, tt.expectedOutput, conf.ToStringMap())
			}
			assert.Equal(t, len(tt.expectedWarnings), len(logs.All()))
			for i, variable := range tt.expectedWarnings {
				errorMsg := fmt.Sprintf(msgTemplate, variable)
				assert.Equal(t, errorMsg, logs.All()[i].Message)
			}
		})
	}
}

func TestNewExpandConverterWithErrors(t *testing.T) {
	var testCases = []struct {
		name          string // test case name (also file name containing config yaml)
		expectedError error
	}{
		{
			name:          "expand-list-error.yaml",
			expectedError: fmt.Errorf("environment variable \"EXTRA_LIST_^VALUE_2\" has invalid name: must match regex %s", envvar.ValidationRegexp),
		},
		{
			name:          "expand-list-map-error.yaml",
			expectedError: fmt.Errorf("environment variable \"EXTRA_LIST_MAP_V#ALUE_2\" has invalid name: must match regex %s", envvar.ValidationRegexp),
		},
		{
			name:          "expand-map-error.yaml",
			expectedError: fmt.Errorf("environment variable \"EX#TRA\" has invalid name: must match regex %s", envvar.ValidationRegexp),
		},
	}

	for _, tt := range testCases {
		t.Run(tt.name, func(t *testing.T) {
			conf, err := confmaptest.LoadConf(filepath.Join("testdata", "errors", tt.name))
			require.NoError(t, err, "Unable to get config")

			// Test that expanded configs are the same with the simple config with no env vars.
			err = createConverter().Convert(context.Background(), conf)

			assert.Equal(t, tt.expectedError, err)
		})
	}
}

func createConverter() confmap.Converter {
	// nolint
	return NewFactory().Create(confmap.ConverterSettings{Logger: zap.NewNop()})
}
