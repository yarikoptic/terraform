package mock

import (
	"fmt"

	"github.com/hashicorp/hcl/v2"
	"github.com/zclconf/go-cty/cty"

	"github.com/hashicorp/terraform/internal/configs/configschema"
	"github.com/hashicorp/terraform/internal/tfdiags"
)

type ComputeValue func(schema *configschema.Attribute, original cty.Value, defaults *DefaultValue) (cty.Value, tfdiags.Diagnostics, bool)

func populateBlock(schema *configschema.Block, object cty.Value, defaults *DefaultValue, computeValue ComputeValue) (cty.Value, tfdiags.Diagnostics) {
	var diags tfdiags.Diagnostics

	if schema == nil {
		panic("must have provided a schema")
	}

	if !object.Type().IsObjectType() {
		panic("can only fill object types from blocks")
	}

	if defaults.Value != cty.NilVal && !defaults.Value.Type().IsObjectType() {
		// The defaults were actually provided by the user, so we return a real
		// set of diagnostics.
		diags = diags.Append(&hcl.Diagnostic{
			Severity: hcl.DiagError,
			Summary:  "Mock provider default value type mismatch",
			Detail:   fmt.Sprintf("Terraform encountered an error applying defaults from a mock provider due to a type mismatch. Expected an object type, but found %s", defaults.Value.Type().FriendlyName()),
			Subject:  defaults.Range.Ptr(),
		})
		return cty.NilVal, diags
	}

	values := make(map[string]cty.Value)

	for name, block := range schema.BlockTypes {

		// Blocks themselves can't be unknown, but attributes within them can
		// be.

		original := object.GetAttr(name)
		if original == cty.NilVal || original.IsNull() {
			values[name] = original
			continue
		}

		childDefaults := defaults.GetChild(name)

		switch block.Nesting {
		case configschema.NestingSingle, configschema.NestingGroup:
			if !original.Type().IsObjectType() {
				panic("invalid object type, expected object but found " + original.Type().GoString())
			}

			// This is the easy case, we have a single block that is going to be
			// a cty.Object so we can just recurse directly.
			var objectDiags tfdiags.Diagnostics
			values[name], objectDiags = populateBlock(&block.Block, original, childDefaults, computeValue)
			diags = diags.Append(objectDiags)

		case configschema.NestingList:
			// Then original is going to be a cty.ListVal, with each entry in
			// the list a block that matches the schema in our block.

			if !original.Type().IsListType() {
				// Something bad has happened, this is a bug in Terraform and
				// shouldn't occur.
				panic("invalid object type, expected nested list but found " + original.Type().GoString())
			}

			if original.LengthInt() == 0 {
				// Empty list block, nothing to compute so return as is.
				values[name] = original
				continue
			}

			// Normal case, let's go over every value in the list and check it
			// for computed values.
			var listValues []cty.Value
			for _, value := range original.AsValueSlice() {
				value, listDiags := populateBlock(&block.Block, value, childDefaults, computeValue)
				diags = diags.Append(listDiags)
				if !listDiags.HasErrors() {
					listValues = append(listValues, value)
				}
			}
			values[name] = cty.ListVal(listValues)

		case configschema.NestingSet:
			// Then original is going to be a cty.SetVal, with each entry in the
			// set a block that matches the schema in our block.

			if !original.Type().IsSetType() {
				// Something bad has happened, this is a bug in Terraform and
				// shouldn't occur.
				panic("invalid object type, expected nested set but found " + original.Type().GoString())
			}

			if original.LengthInt() == 0 {
				// Empty set block, nothing to compute so return as is.
				values[name] = original
				continue
			}

			// Normal case, let's go over every value in the set and check it
			// for computed values.
			var setValues []cty.Value
			for _, value := range original.AsValueSlice() {
				value, setDiags := populateBlock(&block.Block, value, childDefaults, computeValue)
				diags = diags.Append(setDiags)
				if !setDiags.HasErrors() {
					setValues = append(setValues, value)
				}
			}
			values[name] = cty.SetVal(setValues)

		case configschema.NestingMap:
			// Then original is going to be a cty.SetVal, with each entry in the
			// set a block that matches the schema in our block.

			if !original.Type().IsMapType() {
				// Something bad has happened, this is a bug in Terraform and
				// shouldn't occur.
				panic("invalid object type, expected nested map but found " + original.Type().GoString())
			}

			if original.LengthInt() == 0 {
				// Empty set block, nothing to compute so return as is.
				values[name] = original
				continue
			}

			// Normal case, let's go over every value in the set and check it
			// for computed values.
			mapValues := make(map[string]cty.Value)
			for name, value := range original.AsValueMap() {
				value, mapDiags := populateBlock(&block.Block, value, childDefaults, computeValue)
				diags = diags.Append(mapDiags)
				if !mapDiags.HasErrors() {
					mapValues[name] = value
				}
			}
			values[name] = cty.MapVal(mapValues)

		default:
			panic("unrecognized nesting type: " + block.Nesting.String())

		}
	}

	for name, attribute := range schema.Attributes {
		child, childDiags := populateAttribute(attribute, object.GetAttr(name), defaults.GetChild(name), computeValue)
		diags = diags.Append(childDiags)
		if !childDiags.HasErrors() {
			values[name] = child
		}
	}

	return cty.ObjectVal(values), diags
}

func populateAttribute(schema *configschema.Attribute, original cty.Value, defaults *DefaultValue, computeValue ComputeValue) (cty.Value, tfdiags.Diagnostics) {
	value, diags, handled := computeValue(schema, original, defaults)
	if handled || diags.HasErrors() {
		return value, diags
	}

	if schema.NestedType != nil {
		if original == cty.NilVal || original.IsNull() {
			return original, diags
		}

		// NestedTypes may contain nested computed values. We need to recurse
		// and inspect the nested values as well to make sure we get all the
		// potential computed values.

		switch schema.NestedType.Nesting {
		case configschema.NestingSingle, configschema.NestingGroup:
			if !original.Type().IsObjectType() {
				panic("invalid attribute type, expected object but found " + original.Type().GoString())
			}

			attributes := make(map[string]cty.Value)
			for name, attribute := range schema.NestedType.Attributes {
				child, childDiags := populateAttribute(attribute, original.GetAttr(name), defaults.GetChild(name), computeValue)
				diags = diags.Append(childDiags)
				if !childDiags.HasErrors() {
					attributes[name] = child
				}
			}
			return cty.ObjectVal(attributes), diags

		case configschema.NestingList:
			if !original.Type().IsListType() {
				panic("invalid attribute type, expected list but found " + original.Type().GoString())
			}

			if original.LengthInt() == 0 {
				return original, diags
			}

			var listValues []cty.Value
			for _, value := range original.AsValueSlice() {
				attributes := make(map[string]cty.Value)
				for name, attribute := range schema.NestedType.Attributes {
					child, childDiags := populateAttribute(attribute, value.GetAttr(name), defaults.GetChild(name), computeValue)
					diags = diags.Append(childDiags)
					if !childDiags.HasErrors() {
						attributes[name] = child
					}
				}
				listValues = append(listValues, cty.ObjectVal(attributes))
			}
			return cty.ListVal(listValues), diags

		case configschema.NestingSet:
			if !original.Type().IsSetType() {
				panic("invalid attribute type, expected set but found " + original.Type().GoString())
			}

			if original.LengthInt() == 0 {
				return original, diags
			}

			var setValues []cty.Value
			for _, value := range original.AsValueSlice() {
				attributes := make(map[string]cty.Value)
				for name, attribute := range schema.NestedType.Attributes {
					child, childDiags := populateAttribute(attribute, value.GetAttr(name), defaults.GetChild(name), computeValue)
					diags = diags.Append(childDiags)
					if !childDiags.HasErrors() {
						attributes[name] = child
					}
				}
				setValues = append(setValues, cty.ObjectVal(attributes))
			}
			return cty.SetVal(setValues), diags
		case configschema.NestingMap:
			if !original.Type().IsMapType() {
				panic("invalid attribute type, expected map but found " + original.Type().GoString())
			}

			if original.LengthInt() == 0 {
				return original, diags
			}

			mapValues := make(map[string]cty.Value)
			for name, value := range original.AsValueMap() {
				attributes := make(map[string]cty.Value)
				for name, attribute := range schema.NestedType.Attributes {
					child, childDiags := populateAttribute(attribute, value.GetAttr(name), defaults.GetChild(name), computeValue)
					diags = diags.Append(childDiags)
					if !childDiags.HasErrors() {
						attributes[name] = child
					}
				}
				mapValues[name] = cty.ObjectVal(attributes)
			}
			return cty.MapVal(mapValues), diags
		default:
			panic("unrecognized nesting type: " + schema.NestedType.Nesting.String())
		}
	}

	return original, diags
}
