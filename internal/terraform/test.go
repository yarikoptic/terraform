package terraform

import (
	"fmt"
	"github.com/hashicorp/hcl/v2"
	"github.com/hashicorp/terraform/internal/configs"
	"github.com/hashicorp/terraform/internal/lang"
	"github.com/hashicorp/terraform/internal/plans"
	"github.com/hashicorp/terraform/internal/states"
	testresults "github.com/hashicorp/terraform/internal/test"
	"github.com/hashicorp/terraform/internal/tfdiags"
	"github.com/zclconf/go-cty/cty"
	"github.com/zclconf/go-cty/cty/convert"
)

func (c *Context) TestAgainstState(state *states.State, config *configs.Config, run *configs.Run) testresults.Run {
	return c.test(state.SyncWrapper(), plans.NewChanges().SyncWrapper(), config, run, walkApply)
}

func (c *Context) TestAgainstPlan(state *states.State, changes *plans.Plan, config *configs.Config, run *configs.Run) testresults.Run {
	return c.test(state.SyncWrapper(), changes.Changes.SyncWrapper(), config, run, walkPlan)
}

func (c *Context) test(state *states.SyncState, changes *plans.ChangesSync, config *configs.Config, run *configs.Run, operation walkOperation) testresults.Run {
	data := &evaluationStateData{
		Evaluator: &Evaluator{
			Operation: operation,
			Meta:      c.meta,
			Config:    config,
			Plugins:   c.plugins,
			State:     state,
			Changes:   changes,
		},
		ModulePath:      nil, // nil for the root module
		InstanceKeyData: EvalDataForNoInstanceKey,
		Operation:       operation,
	}

	scope := &lang.Scope{
		Data:       data,
		SelfAddr:   nil,
		SourceAddr: nil,
		BaseDir:    ".",
		PureOnly:   operation != walkApply,
	}

	var result testresults.Run
	for _, rule := range run.CheckRules {
		var diags tfdiags.Diagnostics

		refs, moreDiags := lang.ReferencesInExpr(rule.Condition)
		diags = diags.Append(moreDiags)
		moreRefs, moreDiags := lang.ReferencesInExpr(rule.ErrorMessage)
		diags = diags.Append(moreDiags)
		refs = append(refs, moreRefs...)

		hclCtx, moreDiags := scope.EvalContext(refs)
		diags = diags.Append(moreDiags)

		errorMessage, moreDiags := evalCheckErrorMessage(rule.ErrorMessage, hclCtx)
		diags = diags.Append(moreDiags)

		resultVal, hclDiags := rule.Condition.Value(hclCtx)
		diags = diags.Append(hclDiags)

		if diags.HasErrors() {
			result.Errors = result.Errors.Append(diags)
			continue
		}
		result.Warnings = result.Warnings.Append(diags)

		if resultVal.IsNull() {
			result.Errors = result.Errors.Append(&hcl.Diagnostic{
				Severity:    hcl.DiagError,
				Summary:     "Invalid condition result",
				Detail:      "Condition expression must return either true or false, not null.",
				Subject:     rule.Condition.Range().Ptr(),
				Expression:  rule.Condition,
				EvalContext: hclCtx,
			})
			continue
		}

		if !resultVal.IsKnown() {
			result.Errors = result.Errors.Append(&hcl.Diagnostic{
				Severity:    hcl.DiagError,
				Summary:     "Unknown condition result",
				Detail:      "Condition expression could not be evaluated at this time.",
				Subject:     rule.Condition.Range().Ptr(),
				Expression:  rule.Condition,
				EvalContext: hclCtx,
			})
			continue
		}

		var err error
		resultVal, err = convert.Convert(resultVal, cty.Bool)
		if err != nil {
			result.Errors = result.Errors.Append(&hcl.Diagnostic{
				Severity:    hcl.DiagError,
				Summary:     "Invalid condition result",
				Detail:      fmt.Sprintf("Invalid condition result value: %s.", tfdiags.FormatError(err)),
				Subject:     rule.Condition.Range().Ptr(),
				Expression:  rule.Condition,
				EvalContext: hclCtx,
			})
			continue
		}

		if resultVal.True() {
			continue
		}

		result.Failures = result.Failures.Append(&hcl.Diagnostic{
			Severity:    hcl.DiagError,
			Summary:     "Test assertion failed",
			Detail:      errorMessage,
			Subject:     rule.Condition.Range().Ptr(),
			Expression:  rule.Condition,
			EvalContext: hclCtx,
		})
	}
	return result
}
