package mock

import (
	"math/big"

	"github.com/hashicorp/hcl/v2"
	"github.com/zclconf/go-cty/cty"
	"github.com/zclconf/go-cty/cty/convert"

	"github.com/hashicorp/terraform/internal/configs/configschema"
	"github.com/hashicorp/terraform/internal/tfdiags"
)

func ApplyComputedValues(schema *configschema.Block, originals cty.Value, defaults *DefaultValue) (cty.Value, tfdiags.Diagnostics) {
	return populateBlock(schema, originals, defaults, func(schema *configschema.Attribute, original cty.Value, target *DefaultValue) (cty.Value, tfdiags.Diagnostics, bool) {
		return computeValueFromAttribute(original, target)
	})
}

func PlanComputedValues(schema *configschema.Block, originals cty.Value) (cty.Value, tfdiags.Diagnostics) {
	return populateBlock(schema, originals, &DefaultValue{Value: cty.NilVal}, func(schema *configschema.Attribute, original cty.Value, _ *DefaultValue) (cty.Value, tfdiags.Diagnostics, bool) {
		if !schema.Computed {
			// Then whatever the value is we don't care.
			return original, nil, false
		}

		if original != cty.NilVal && !original.IsNull() {
			// Then we already have a value for this, so we'll return that value
			// directly.
			return original, nil, false
		}

		// At this stage, we just create an unknown value.
		return cty.UnknownVal(getTypeFromAttribute(schema)), nil, true
	})
}

func computeValueFromAttribute(original cty.Value, target *DefaultValue) (cty.Value, tfdiags.Diagnostics, bool) {
	var diags tfdiags.Diagnostics

	// If the original is known, then we don't need to compute anything.
	if original.IsKnown() {
		return original, diags, false
	}

	if target.Value != cty.NilVal {

		converted, err := convert.Convert(target.Value, original.Type())
		if err != nil {
			diags = diags.Append(&hcl.Diagnostic{
				Severity: hcl.DiagError,
				Summary:  "Mock provider default value type mismatch",
				Detail:   "",
				Subject:  target.Range.Ptr(),
			})
		}

		// Then return this target specifically.
		return converted, diags, true
	}

	var value cty.Value
	switch {
	case original.Type().IsPrimitiveType():
		switch original.Type() {
		case cty.String:
			// Random 8 character string.
			value = cty.StringVal(generateString(8))
		case cty.Number:
			// Just return 0.
			value = cty.NumberVal(big.NewFloat(0))
		case cty.Bool:
			// Just return false.
			value = cty.False
		default:
			panic("unrecognized attribute type: " + original.Type().GoString())
		}
	case original.Type().IsMapType():
		// A collection is easy, we'll just return an empty one.
		value = cty.MapValEmpty(original.Type().ElementType())
	case original.Type().IsSetType():
		// A collection is easy, we'll just return an empty one.
		value = cty.SetValEmpty(original.Type().ElementType())
	case original.Type().IsListType():
		// A collection is easy, we'll just return an empty one.
		value = cty.MapValEmpty(original.Type().ElementType())
	case original.Type().IsObjectType():
		// An object value is a bit more tricky, as we need to generate
		// acceptable values for every attribute.
		attrs := make(map[string]cty.Value)
		for name, attr := range original.Type().AttributeTypes() {
			// Generate a value for this attribute recursively.
			child, childDiags, _ := computeValueFromAttribute(cty.UnknownVal(attr), &DefaultValue{Value: cty.NilVal})
			diags = diags.Append(childDiags)
			if !childDiags.HasErrors() {
				attrs[name] = child
			}
		}
		value = cty.ObjectVal(attrs)
	default:
		panic("unrecognized attribute type: " + original.Type().GoString())
	}
	return value, diags, true
}

func getTypeFromAttribute(attribute *configschema.Attribute) cty.Type {
	if attribute.NestedType != nil {

		types := make(map[string]cty.Type)
		for name, attribute := range attribute.NestedType.Attributes {
			types[name] = getTypeFromAttribute(attribute)
		}
		obj := cty.Object(types)

		switch attribute.NestedType.Nesting {
		case configschema.NestingSingle, configschema.NestingGroup:
			return obj
		case configschema.NestingList:
			return cty.List(obj)
		case configschema.NestingSet:
			return cty.Set(obj)
		case configschema.NestingMap:
			return cty.Map(obj)
		default:
			panic("unrecognized nesting type: " + attribute.NestedType.Nesting.String())
		}

	}

	return attribute.Type
}
