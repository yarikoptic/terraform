package providers

import (
	"fmt"

	"github.com/hashicorp/hcl/v2/hclparse"
	"github.com/zclconf/go-cty/cty"

	"github.com/hashicorp/terraform/internal/providers/mock"
	"github.com/hashicorp/terraform/internal/tfdiags"
)

var _ Interface = (*Mock)(nil)

type Mock struct {
	Provider Interface
	Parser   *hclparse.Parser
	Source   string

	resources *mock.Resources
	schema    *GetProviderSchemaResponse
}

func (m *Mock) GetProviderSchema() GetProviderSchemaResponse {
	if m.schema == nil {
		response := m.Provider.GetProviderSchema()
		if response.Diagnostics.HasErrors() {
			// Don't cache the response if it errored.
			return response
		}

		m.schema = &response
	}

	return *m.schema
}

func (m *Mock) ValidateProviderConfig(request ValidateProviderConfigRequest) ValidateProviderConfigResponse {
	// Pass this directly onto the provider.
	return m.Provider.ValidateProviderConfig(request)
}

func (m *Mock) ValidateResourceConfig(request ValidateResourceConfigRequest) ValidateResourceConfigResponse {
	// Pass this directly onto the provider.
	return m.Provider.ValidateResourceConfig(request)
}

func (m *Mock) ValidateDataResourceConfig(request ValidateDataResourceConfigRequest) ValidateDataResourceConfigResponse {
	// Pass this directly onto the provider.
	return m.Provider.ValidateDataResourceConfig(request)
}

func (m *Mock) UpgradeResourceState(request UpgradeResourceStateRequest) UpgradeResourceStateResponse {
	// Pass this directly onto the provider.
	return m.Provider.UpgradeResourceState(request)
}

func (m *Mock) ConfigureProvider(request ConfigureProviderRequest) ConfigureProviderResponse {
	var diags tfdiags.Diagnostics
	m.resources, diags = mock.DecodeSourceFile(m.Parser, m.Source)
	return ConfigureProviderResponse{
		Diagnostics: diags,
	}
}

func (m *Mock) Stop() error {
	return m.Provider.Stop()
}

func (m *Mock) ReadResource(request ReadResourceRequest) ReadResourceResponse {
	// For the mocked provider, we just pass back whatever we have in the state.
	return ReadResourceResponse{
		NewState: request.PriorState,
	}
}

func (m *Mock) PlanResourceChange(request PlanResourceChangeRequest) PlanResourceChangeResponse {
	if request.ProposedNewState == cty.NilVal || request.ProposedNewState.IsNull() {
		// Then we are deleting this resource, so just return it as is.
		return PlanResourceChangeResponse{
			PlannedState:   request.ProposedNewState,
			PlannedPrivate: []byte("destroy"),
		}
	}

	if request.PriorState == cty.NilVal || request.PriorState.IsNull() {
		var diags tfdiags.Diagnostics

		// Then we are creating this resource. Let's look for any computed
		// values and populate them.

		schema := m.GetProviderSchema()
		diags = diags.Append(schema.Diagnostics)
		if schema.Diagnostics.HasErrors() {
			response := PlanResourceChangeResponse{}
			response.Diagnostics = response.Diagnostics.Append(tfdiags.Sourceless(tfdiags.Error, "Failed to retrieve provider schema", "The mocked provider failed to retrieve the original provider schema. More details will follow in subsequent diagnostics."))
			response.Diagnostics = response.Diagnostics.Append(diags)
			return response
		}

		resource, exists := schema.ResourceTypes[request.TypeName]
		if !exists {
			diags = diags.Append(tfdiags.Sourceless(tfdiags.Error, "Failed to retrieve resource schema", fmt.Sprintf("The mocked provider failed to retrieve the resource schema for %s. This is a bug in Terraform; please report it.", request.TypeName)))
			return PlanResourceChangeResponse{
				Diagnostics: diags,
			}
		}

		value, mockDiags := mock.PlanComputedValues(resource.Block, request.ProposedNewState)
		diags = diags.Append(mockDiags)
		return PlanResourceChangeResponse{
			PlannedState:   value,
			PlannedPrivate: []byte("create"),
			Diagnostics:    diags,
		}
	}

	// Otherwise, we are just updating a resource that already exists. So we'll
	// just return whatever the new proposed state is.
	return PlanResourceChangeResponse{
		PlannedState:   request.ProposedNewState,
		PlannedPrivate: []byte("update"),
	}
}

func (m *Mock) ApplyResourceChange(request ApplyResourceChangeRequest) ApplyResourceChangeResponse {
	switch string(request.PlannedPrivate) {
	case "create":
		var diags tfdiags.Diagnostics

		schema := m.GetProviderSchema()
		diags = diags.Append(schema.Diagnostics)
		if schema.Diagnostics.HasErrors() {
			response := ApplyResourceChangeResponse{}
			response.Diagnostics = response.Diagnostics.Append(tfdiags.Sourceless(tfdiags.Error, "Failed to retrieve provider schema", "The mocked provider failed to retrieve the original provider schema. More details will follow in subsequent diagnostics."))
			response.Diagnostics = response.Diagnostics.Append(diags)
			return response
		}

		resource, exists := schema.ResourceTypes[request.TypeName]
		if !exists {
			diags = diags.Append(tfdiags.Sourceless(tfdiags.Error, "Failed to retrieve resource schema", fmt.Sprintf("The mocked provider failed to retrieve the resource schema for %s. This is a bug in Terraform; please report it.", request.TypeName)))
			return ApplyResourceChangeResponse{
				Diagnostics: diags,
			}
		}

		// Pass in our default value.
		var defaults *mock.DefaultValue
		if resource, ok := m.resources.Resources[request.TypeName]; ok {
			defaults = resource.Default
		} else {
			defaults = &mock.DefaultValue{
				Value: cty.NilVal,
			}
		}

		// We need to look for any unknown values and give them a value.
		response, mockDiags := mock.ApplyComputedValues(resource.Block, request.PlannedState, defaults)
		return ApplyResourceChangeResponse{
			NewState:    response,
			Diagnostics: diags.Append(mockDiags),
		}

	default:
		return ApplyResourceChangeResponse{
			NewState: request.PlannedState,
		}
	}
}

func (m *Mock) ImportResourceState(request ImportResourceStateRequest) ImportResourceStateResponse {
	// TODO: Figure out how to simulate external resources in mocks.
	response := ImportResourceStateResponse{}
	response.Diagnostics = response.Diagnostics.Append(tfdiags.Sourceless(tfdiags.Error, "Invalid import request", "Cannot import resources from mock providers"))
	return response
}

func (m *Mock) ReadDataSource(request ReadDataSourceRequest) ReadDataSourceResponse {
	// TODO: Figure out how to simulate data sources in mocks.
	response := ReadDataSourceResponse{}
	response.Diagnostics = response.Diagnostics.Append(tfdiags.Sourceless(tfdiags.Error, "Invalid data source", "Cannot read data sources from mocked providers"))
	return response
}

func (m *Mock) Close() error {
	return m.Provider.Close()
}
