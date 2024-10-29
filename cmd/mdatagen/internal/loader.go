// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package internal // import "go.opentelemetry.io/collector/cmd/mdatagen/internal"

import (
	"context"
	"errors"
	"fmt"
	"os/exec"
	"path/filepath"
	"strings"

	"go.opentelemetry.io/collector/component"
	"go.opentelemetry.io/collector/config/configtelemetry"
	"go.opentelemetry.io/collector/confmap"
	"go.opentelemetry.io/collector/confmap/confmaptest"
	"go.opentelemetry.io/collector/confmap/provider/fileprovider"
	"go.opentelemetry.io/collector/filter"
	"go.opentelemetry.io/collector/pdata/pcommon"
)

type MetricName string

func (mn MetricName) Render() (string, error) {
	return FormatIdentifier(string(mn), true)
}

func (mn MetricName) RenderUnexported() (string, error) {
	return FormatIdentifier(string(mn), false)
}

type AttributeName string

func (mn AttributeName) Render() (string, error) {
	return FormatIdentifier(string(mn), true)
}

func (mn AttributeName) RenderUnexported() (string, error) {
	return FormatIdentifier(string(mn), false)
}

// ValueType defines an attribute value type.
type ValueType struct {
	// ValueType is type of the attribute value.
	ValueType pcommon.ValueType
}

// UnmarshalText implements the encoding.TextUnmarshaler interface.
func (mvt *ValueType) UnmarshalText(text []byte) error {
	switch vtStr := string(text); vtStr {
	case "string":
		mvt.ValueType = pcommon.ValueTypeStr
	case "int":
		mvt.ValueType = pcommon.ValueTypeInt
	case "double":
		mvt.ValueType = pcommon.ValueTypeDouble
	case "bool":
		mvt.ValueType = pcommon.ValueTypeBool
	case "bytes":
		mvt.ValueType = pcommon.ValueTypeBytes
	case "slice":
		mvt.ValueType = pcommon.ValueTypeSlice
	case "map":
		mvt.ValueType = pcommon.ValueTypeMap
	default:
		return fmt.Errorf("invalid type: %q", vtStr)
	}
	return nil
}

// String returns capitalized name of the ValueType.
func (mvt ValueType) String() string {
	return strings.Title(strings.ToLower(mvt.ValueType.String())) // nolint SA1019
}

// Primitive returns name of primitive type for the ValueType.
func (mvt ValueType) Primitive() string {
	switch mvt.ValueType {
	case pcommon.ValueTypeStr:
		return "string"
	case pcommon.ValueTypeInt:
		return "int64"
	case pcommon.ValueTypeDouble:
		return "float64"
	case pcommon.ValueTypeBool:
		return "bool"
	case pcommon.ValueTypeBytes:
		return "[]byte"
	case pcommon.ValueTypeSlice:
		return "[]any"
	case pcommon.ValueTypeMap:
		return "map[string]any"
	case pcommon.ValueTypeEmpty:
		return ""
	default:
		return ""
	}
}

type stability struct {
	Level string `mapstructure:"level"`
	From  string `mapstructure:"from"`
}

func (s stability) String() string {
	if len(s.Level) == 0 || strings.EqualFold(s.Level, component.StabilityLevelStable.String()) {
		return ""
	}
	if len(s.From) > 0 {
		return fmt.Sprintf(" [%s since %s]", s.Level, s.From)
	}
	return fmt.Sprintf(" [%s]", s.Level)
}

type Metric struct {
	// Enabled defines whether the metric is enabled by default.
	Enabled bool `mapstructure:"enabled"`

	// Warnings that will be shown to user under specified conditions.
	Warnings warnings `mapstructure:"warnings"`

	// Description of the metric.
	Description string `mapstructure:"description"`

	// The stability level of the metric.
	Stability stability `mapstructure:"stability"`

	// ExtendedDocumentation of the metric. If specified, this will
	// be appended to the description used in generated documentation.
	ExtendedDocumentation string `mapstructure:"extended_documentation"`

	// Optional can be used to specify metrics that may
	// or may not be present in all cases, depending on configuration.
	Optional bool `mapstructure:"optional"`

	// Unit of the metric.
	Unit *string `mapstructure:"unit"`

	// Sum stores metadata for sum metric type
	Sum *sum `mapstructure:"sum,omitempty"`
	// Gauge stores metadata for gauge metric type
	Gauge *gauge `mapstructure:"gauge,omitempty"`
	// Histogram stores metadata for histogram metric type
	Histogram *histogram `mapstructure:"histogram,omitempty"`

	// Attributes is the list of attributes that the metric emits.
	Attributes []AttributeName `mapstructure:"attributes"`

	// Level specifies the minimum `configtelemetry.Level` for which
	// the metric will be emitted. This only applies to internal telemetry
	// configuration.
	Level configtelemetry.Level `mapstructure:"level"`
}

func (m *Metric) Unmarshal(parser *confmap.Conf) error {
	if !parser.IsSet("enabled") {
		return errors.New("missing required field: `enabled`")
	}
	return parser.Unmarshal(m)
}
func (m Metric) Data() MetricData {
	if m.Sum != nil {
		return m.Sum
	}
	if m.Gauge != nil {
		return m.Gauge
	}
	if m.Histogram != nil {
		return m.Histogram
	}
	return nil
}

type warnings struct {
	// A warning that will be displayed if the field is enabled in user config.
	IfEnabled string `mapstructure:"if_enabled"`
	// A warning that will be displayed if `enabled` field is not set explicitly in user config.
	IfEnabledNotSet string `mapstructure:"if_enabled_not_set"`
	// A warning that will be displayed if the field is configured by user in any way.
	IfConfigured string `mapstructure:"if_configured"`
}

type Attribute struct {
	// Description describes the purpose of the attribute.
	Description string `mapstructure:"description"`
	// NameOverride can be used to override the attribute name.
	NameOverride string `mapstructure:"name_override"`
	// Enabled defines whether the attribute is enabled by default.
	Enabled bool `mapstructure:"enabled"`
	// Include can be used to filter attributes.
	Include []filter.Config `mapstructure:"include"`
	// Include can be used to filter attributes.
	Exclude []filter.Config `mapstructure:"exclude"`
	// Enum can optionally describe the set of values to which the attribute can belong.
	Enum []string `mapstructure:"enum"`
	// Type is an attribute type.
	Type ValueType `mapstructure:"type"`
	// FullName is the attribute name populated from the map key.
	FullName AttributeName `mapstructure:"-"`
	// Warnings that will be shown to user under specified conditions.
	Warnings warnings `mapstructure:"warnings"`
}

// Name returns actual name of the attribute that is set on the metric after applying NameOverride.
func (a Attribute) Name() AttributeName {
	if a.NameOverride != "" {
		return AttributeName(a.NameOverride)
	}
	return a.FullName
}

func (a Attribute) TestValue() string {
	if a.Enum != nil {
		return fmt.Sprintf(`"%s"`, a.Enum[0])
	}
	switch a.Type.ValueType {
	case pcommon.ValueTypeEmpty:
		return ""
	case pcommon.ValueTypeStr:
		return fmt.Sprintf(`"%s-val"`, a.FullName)
	case pcommon.ValueTypeInt:
		return fmt.Sprintf("%d", len(a.FullName))
	case pcommon.ValueTypeDouble:
		return fmt.Sprintf("%f", 0.1+float64(len(a.FullName)))
	case pcommon.ValueTypeBool:
		return fmt.Sprintf("%t", len(a.FullName)%2 == 0)
	case pcommon.ValueTypeMap:
		return fmt.Sprintf(`map[string]any{"key1": "%s-val1", "key2": "%s-val2"}`, a.FullName, a.FullName)
	case pcommon.ValueTypeSlice:
		return fmt.Sprintf(`[]any{"%s-item1", "%s-item2"}`, a.FullName, a.FullName)
	case pcommon.ValueTypeBytes:
		return fmt.Sprintf(`[]byte("%s-val")`, a.FullName)
	}
	return ""
}

type ignore struct {
	Top []string `mapstructure:"top"`
	Any []string `mapstructure:"any"`
}

type goLeak struct {
	Skip     bool   `mapstructure:"skip"`
	Ignore   ignore `mapstructure:"ignore"`
	Setup    string `mapstructure:"setup"`
	Teardown string `mapstructure:"teardown"`
}

type tests struct {
	Config              any    `mapstructure:"config"`
	SkipLifecycle       bool   `mapstructure:"skip_lifecycle"`
	SkipShutdown        bool   `mapstructure:"skip_shutdown"`
	GoLeak              goLeak `mapstructure:"goleak"`
	ExpectConsumerError bool   `mapstructure:"expect_consumer_error"`
	Host                string `mapstructure:"host"`
}

type telemetry struct {
	Level   configtelemetry.Level `mapstructure:"level"`
	Metrics map[MetricName]Metric `mapstructure:"metrics"`
}

func (t telemetry) Levels() map[string]interface{} {
	levels := map[string]interface{}{}
	for _, m := range t.Metrics {
		levels[m.Level.String()] = nil
	}
	return levels
}

type Metadata struct {
	// Type of the component.
	Type string `mapstructure:"type"`
	// Type of the parent component (applicable to subcomponents).
	Parent string `mapstructure:"parent"`
	// Status information for the component.
	Status *Status `mapstructure:"status"`
	// The name of the package that will be generated.
	GeneratedPackageName string `mapstructure:"generated_package_name"`
	// Telemetry information for the component.
	Telemetry telemetry `mapstructure:"telemetry"`
	// SemConvVersion is a version number of OpenTelemetry semantic conventions applied to the scraped metrics.
	SemConvVersion string `mapstructure:"sem_conv_version"`
	// ResourceAttributes that can be emitted by the component.
	ResourceAttributes map[AttributeName]Attribute `mapstructure:"resource_attributes"`
	// Attributes emitted by one or more metrics.
	Attributes map[AttributeName]Attribute `mapstructure:"attributes"`
	// Metrics that can be emitted by the component.
	Metrics map[MetricName]Metric `mapstructure:"metrics"`
	// GithubProject is the project where the component README lives in the format of org/repo, defaults to open-telemetry/opentelemetry-collector-contrib
	GithubProject string `mapstructure:"github_project"`
	// ScopeName of the metrics emitted by the component.
	ScopeName string `mapstructure:"scope_name"`
	// ShortFolderName is the shortened folder name of the component, removing class if present
	ShortFolderName string `mapstructure:"-"`
	// Tests is the set of tests generated with the component
	Tests tests `mapstructure:"tests"`
}

func setAttributesFullName(attrs map[AttributeName]Attribute) {
	for k, v := range attrs {
		v.FullName = k
		attrs[k] = v
	}
}

type TemplateContext struct {
	Metadata
	// Package name for generated code.
	Package string
}

func LoadMetadata(filePath string) (Metadata, error) {
	cp, err := fileprovider.NewFactory().Create(confmaptest.NewNopProviderSettings()).Retrieve(context.Background(), "file:"+filePath, nil)
	if err != nil {
		return Metadata{}, err
	}

	conf, err := cp.AsConf()
	if err != nil {
		return Metadata{}, err
	}

	md := Metadata{ShortFolderName: shortFolderName(filePath), Tests: tests{Host: "componenttest.NewNopHost()"}}
	if err = conf.Unmarshal(&md); err != nil {
		return md, err
	}
	if md.ScopeName == "" {
		md.ScopeName, err = packageName()
		if err != nil {
			return md, err
		}
	}
	if md.GeneratedPackageName == "" {
		md.GeneratedPackageName = "metadata"
	}

	if err = md.Validate(); err != nil {
		return md, err
	}

	setAttributesFullName(md.Attributes)
	setAttributesFullName(md.ResourceAttributes)

	return md, nil
}

var componentTypes = []string{
	"connector",
	"exporter",
	"extension",
	"processor",
	"scraper",
	"receiver",
}

func shortFolderName(filePath string) string {
	parentFolder := filepath.Base(filepath.Dir(filePath))
	for _, cType := range componentTypes {
		if strings.HasSuffix(parentFolder, cType) {
			return strings.TrimSuffix(parentFolder, cType)
		}
	}
	return parentFolder
}

func packageName() (string, error) {
	cmd := exec.Command("go", "list", "-f", "{{.ImportPath}}")
	output, err := cmd.Output()
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(output)), nil
}
