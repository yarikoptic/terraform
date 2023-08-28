package mock

import (
	"fmt"

	"github.com/hashicorp/hcl/v2"
	"github.com/hashicorp/hcl/v2/hclparse"
	"github.com/zclconf/go-cty/cty"

	"github.com/hashicorp/terraform/internal/tfdiags"
)

var (
	FileSchema = &hcl.BodySchema{
		Blocks: []hcl.BlockHeaderSchema{
			{
				Type:       "resource",
				LabelNames: []string{"type"},
			},
			{
				Type:       "data",
				LabelNames: []string{"type"},
			},
		},
	}

	ResourceSchema = &hcl.BodySchema{
		Attributes: []hcl.AttributeSchema{
			{
				Name: "default",
			},
			{
				Name: "overrides",
			},
		},
	}
)

// Resources contains all the known resource and data source types that have
// been provided for this provider.
type Resources struct {
	Resources   map[string]*Resource
	DataSources map[string]*Resource
}

// Resource describes a single resource or data source within a mocked provider.
type Resource struct {
	// Type is the resource type name.
	Type string

	// TypeRange is the range where Type was defined.
	TypeRange hcl.Range

	// Default is the default value applied to every resource of this Type that
	// aren't included in the Overrides map.
	Default *DefaultValue
}

// DefaultValue wraps
type DefaultValue struct {
	// Value is the default value that should be applied for this value.
	Value cty.Value

	// Range is the range that this value was specified in the original
	// configuration.
	Range hcl.Range
}

func (value *DefaultValue) GetChild(attr string) *DefaultValue {
	child := &DefaultValue{
		Value: cty.NilVal,
		Range: value.Range,
	}

	if value.Value == cty.NilVal {
		return child
	}

	if !value.Value.Type().HasAttribute(attr) {
		return child
	}

	child.Value = value.Value.GetAttr(attr)
	return child
}

func DecodeSourceFile(parser *hclparse.Parser, path string) (*Resources, tfdiags.Diagnostics) {
	var diags tfdiags.Diagnostics

	if len(path) == 0 {
		return &Resources{
			Resources:   make(map[string]*Resource),
			DataSources: make(map[string]*Resource),
		}, diags
	}

	file, fileDiags := parser.ParseHCLFile(path)
	diags = diags.Append(fileDiags)
	if fileDiags.HasErrors() {
		return nil, diags
	}

	content, contentDiags := file.Body.Content(FileSchema)
	diags = diags.Append(contentDiags)

	resources := &Resources{
		Resources:   make(map[string]*Resource),
		DataSources: make(map[string]*Resource),
	}

	for _, block := range content.Blocks {

		content, contentDiags := block.Body.Content(ResourceSchema)
		diags = diags.Append(contentDiags)

		resource := &Resource{
			Type:      block.Labels[0],
			TypeRange: block.LabelRanges[0],
			Default: &DefaultValue{
				Value: cty.NilVal,
			},
		}

		if attr, exists := content.Attributes["default"]; exists {
			defaults, defaultDiags := attr.Expr.Value(nil)
			diags = diags.Append(defaultDiags)

			if !diags.HasErrors() {
				resource.Default.Value = defaults
				resource.Default.Range = attr.Range

				if !resource.Default.Value.Type().IsObjectType() {
					diags = diags.Append(&hcl.Diagnostic{
						Severity: hcl.DiagError,
						Summary:  "Invalid default type",
						Detail:   fmt.Sprintf("The default value for %q should be an object, but was a %q.", resource.Type, resource.Default.Value.Type().FriendlyName()),
						Subject:  resource.Default.Range.Ptr(),
					})
				}
			}
		}

		switch block.Type {
		case "resource":
			if duplicate, exists := resources.Resources[resource.Type]; exists {
				diags = diags.Append(&hcl.Diagnostic{
					Severity: hcl.DiagError,
					Summary:  "Duplicate resource definition",
					Detail:   fmt.Sprintf("Values for the resource %q were already provided at %s. You can only provide values for a resource once.", resource.Type, duplicate.TypeRange),
					Subject:  block.LabelRanges[0].Ptr(),
				})
			} else {
				resources.Resources[resource.Type] = resource
			}
		case "data":
			if duplicate, exists := resources.DataSources[resource.Type]; exists {
				diags = diags.Append(&hcl.Diagnostic{
					Severity: hcl.DiagError,
					Summary:  "Duplicate data source definition",
					Detail:   fmt.Sprintf("Values for the data source %q were already provided at %s. You can only provide values for a data source once.", resource.Type, duplicate.TypeRange),
					Subject:  block.LabelRanges[0].Ptr(),
				})
			} else {
				resources.DataSources[resource.Type] = resource
			}
		}
	}

	return resources, diags
}
