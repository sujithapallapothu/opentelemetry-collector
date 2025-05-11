// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package otelcol // import "go.opentelemetry.io/collector/otelcol"

import (
	"fmt"
	"go.opentelemetry.io/collector/confmap"
	"go.opentelemetry.io/collector/connector"
	"go.opentelemetry.io/collector/exporter"
	"go.opentelemetry.io/collector/extension"
	"go.opentelemetry.io/collector/otelcol/internal/configunmarshaler"
	"go.opentelemetry.io/collector/processor"
	"go.opentelemetry.io/collector/receiver"
	"go.opentelemetry.io/collector/service"
	"go.opentelemetry.io/collector/service/telemetry"
)

type configSettings struct {
	Receivers  *configunmarshaler.Configs[receiver.Factory]  `mapstructure:"receivers"`
	Processors *configunmarshaler.Configs[processor.Factory] `mapstructure:"processors"`
	Exporters  *configunmarshaler.Configs[exporter.Factory]  `mapstructure:"exporters"`
	Connectors *configunmarshaler.Configs[connector.Factory] `mapstructure:"connectors"`
	Extensions *configunmarshaler.Configs[extension.Factory] `mapstructure:"extensions"`
	Service    service.Config                                `mapstructure:"service"`
}

// unmarshal the configSettings from a confmap.Conf.
// After the config is unmarshalled, `Validate()` must be called to validate.
func unmarshal(v *confmap.Conf, factories Factories) (*configSettings, error) {
	fmt.Println("#######unmarshal factories  2", factories.Receivers)
	telFactory := telemetry.NewFactory()
	defaultTelConfig := *telFactory.CreateDefaultConfig().(*telemetry.Config)

	Receivers := configunmarshaler.NewConfigs(factories.Receivers)
	Processors := configunmarshaler.NewConfigs(factories.Processors)
	Exporters := configunmarshaler.NewConfigs(factories.Exporters)
	Connectors := configunmarshaler.NewConfigs(factories.Connectors)
	Extensions := configunmarshaler.NewConfigs(factories.Extensions)
	fmt.Println("#######unmarshal cfg - 4", Receivers)
	fmt.Println("#######unmarshal cfg - 5", defaultTelConfig.Logs.InitialFields)

	// Unmarshal top level sections and validate.
	cfg := &configSettings{
		Receivers:  Receivers,
		Processors: Processors,
		Exporters:  Exporters,
		Connectors: Connectors,
		Extensions: Extensions,
		// TODO: Add a component.ServiceFactory to allow this to be defined by the Service.
		Service: service.Config{
			Telemetry: defaultTelConfig,
		},
	}

	return cfg, v.Unmarshal(&cfg)
}
