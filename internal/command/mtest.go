package command

import (
	"fmt"
	"github.com/hashicorp/hcl/v2"
	"github.com/hashicorp/terraform/internal/addrs"
	"github.com/hashicorp/terraform/internal/backend"
	"github.com/hashicorp/terraform/internal/command/arguments"
	"github.com/hashicorp/terraform/internal/command/views"
	"github.com/hashicorp/terraform/internal/configs"
	"github.com/hashicorp/terraform/internal/configs/configload"
	"github.com/hashicorp/terraform/internal/plans"
	"github.com/hashicorp/terraform/internal/states"
	"github.com/hashicorp/terraform/internal/terraform"
	testresults "github.com/hashicorp/terraform/internal/test"
	"github.com/hashicorp/terraform/internal/tfdiags"
)

type MTestCommand struct {
	Meta

	loader *configload.Loader
}

func (c *MTestCommand) Help() string {
	return "fill me"
}

func (c *MTestCommand) Synopsis() string {
	return "execute tests for the current module"
}

func (c *MTestCommand) Run(rawArgs []string) int {
	var diags tfdiags.Diagnostics

	common, rawArgs := arguments.ParseView(rawArgs)
	c.View.Configure(common)

	view := views.NewMTest(c.View)

	loader, err := c.initConfigLoader()
	diags = diags.Append(err)
	if err != nil {
		view.Diagnostics(diags)
		return 1
	}
	c.loader = loader

	config, configDiags := loader.LoadConfigWithTests(".")
	diags = diags.Append(configDiags)
	if configDiags.HasErrors() {
		view.Diagnostics(diags)
		return 1
	}

	opts, err := c.contextOpts()
	diags = diags.Append(err)
	if err != nil {
		view.Diagnostics(diags)
		return 1
	}

	ctx, diags := terraform.NewContext(opts)
	if diags.HasErrors() {
		view.Diagnostics(diags)
		return 1
	}

	var files testresults.Files
	for key, file := range config.Module.Tests {
		view.Start(key)
		result := c.runTestFile(ctx, key, file, config)
		view.Complete(key, result)

		files.TotalCount = files.TotalCount + result.TotalCount
		files.SkippedCount = files.SkippedCount + result.SkippedCount
		files.ErrorCount = files.ErrorCount + result.ErrorCount
		files.FailureCount = files.FailureCount + result.FailureCount
		files.Files = append(files.Files, result)

	}

	view.Summary(files)
	view.Diagnostics(diags) // Finally, print out any warnings.
	return 0
}

func (c *MTestCommand) runTestFile(ctx *terraform.Context, key string, test *configs.TestFile, config *configs.Config) testresults.File {
	result := testresults.File{
		Path:       key,
		TotalCount: len(test.Runs),
	}
	state := states.NewState()

	defer func() {
		variables, variableDiags := c.stubVariables(test, config)
		if variableDiags.HasErrors() {
			c.Ui.Error(fmt.Sprintf("failed destroy %s", variableDiags))
		}

		destroyPlan, destroyPlanDiags := ctx.Plan(config, state, &terraform.PlanOpts{
			Mode:         plans.DestroyMode,
			SetVariables: variables,
		})
		if destroyPlanDiags.HasErrors() {
			c.Ui.Error(fmt.Sprintf("failed destroy plan %s", destroyPlanDiags))
		}

		finalState, destroyDiags := ctx.Apply(destroyPlan, config)
		if destroyDiags.HasErrors() {
			c.Ui.Error(fmt.Sprintf("failed destroy %s", destroyDiags))
			for _, resource := range finalState.AllResourceInstanceObjectAddrs() {
				c.Ui.Error(fmt.Sprintf("  failed destroy %s", resource.Instance.String()))
			}
		}
	}()

	for ix, run := range test.Runs {
		newState, runResult := c.run(ctx, state, run, test, config)

		result.Runs = append(result.Runs, runResult)
		if len(runResult.Errors) > 0 {
			// Then we're going to give up, there's no point trying to complete
			// the other runs.
			result.ErrorCount++
			result.SkippedCount = result.TotalCount - (ix + 1)
			return result
		}

		if len(runResult.Failures) > 0 {
			result.FailureCount++
		}

		state = newState
	}
	return result
}

func (c *MTestCommand) run(ctx *terraform.Context, state *states.State, run *configs.Run, test *configs.TestFile, config *configs.Config) (*states.State, testresults.Run) {
	result := testresults.Run{}

	if len(run.Module) > 0 {
		moduleConfig, diags := c.loader.LoadConfig(run.Module)
		if diags.HasErrors() {
			result.Errors = result.Errors.Append(diags)
			return state, result
		}
		result.Warnings = result.Warnings.Append(diags)
		config = moduleConfig
	}

	var targets []addrs.Targetable
	for _, target := range run.Target {
		addr, diags := addrs.ParseTarget(target)
		if diags.HasErrors() {
			result.Errors = result.Errors.Append(diags)
			return state, result
		}
		result.Warnings = result.Warnings.Append(diags)
		targets = append(targets, addr.Subject)
	}

	var replaces []addrs.AbsResourceInstance
	for _, replace := range run.Replace {
		addr, diags := addrs.ParseAbsResourceInstance(replace)
		if diags.HasErrors() {
			result.Errors = result.Errors.Append(diags)
			return state, result
		}
		result.Warnings = result.Warnings.Append(diags)

		if addr.Resource.Resource.Mode != addrs.ManagedResourceMode {
			result.Errors = result.Errors.Append(hcl.Diagnostic{
				Severity: hcl.DiagError,
				Summary:  "can only target managed resources for forced replacements",
				Detail:   addr.String(),
				Subject:  replace.SourceRange().Ptr(),
			})
			return state, result
		}

		replaces = append(replaces, addr)
	}

	variables, variableDiags := c.collateVariables(test, run, config)
	if variableDiags.HasErrors() {
		result.Errors = result.Errors.Append(variableDiags)
		return state, result
	}
	result.Warnings = result.Warnings.Append(variableDiags)

	plan, diags := ctx.Plan(config, state, &terraform.PlanOpts{
		Mode: func() plans.Mode {
			switch run.Mode {
			case configs.RefreshOnlyTestMode:
				return plans.RefreshOnlyMode
			default:
				return plans.NormalMode
			}
		}(),
		SetVariables: variables,
		Targets:      targets,
		ForceReplace: replaces,
	})
	if diags.HasErrors() {
		result.Errors = result.Errors.Append(diags)
		return state, result
	}
	result.Warnings = result.Warnings.Append(diags)
	result.Plan = plan

	if run.Command == configs.ApplyTestCommand {
		state, diags = ctx.Apply(plan, config)
		if diags.HasErrors() {
			result.Errors = result.Errors.Append(diags)
			return state, result
		}
		result.Warnings = result.Warnings.Append(diags)

		testResult := ctx.TestAgainstState(state, config, run)
		result.Errors = result.Errors.Append(testResult.Errors)
		result.Failures = result.Failures.Append(testResult.Failures)
		result.Warnings = result.Warnings.Append(testResult.Warnings)

		return state, result
	}

	testResult := ctx.TestAgainstPlan(state, plan, config, run)
	result.Errors = result.Errors.Append(testResult.Errors)
	result.Failures = result.Failures.Append(testResult.Failures)
	result.Warnings = result.Warnings.Append(testResult.Warnings)
	return state, result
}

func (c *MTestCommand) stubVariables(test *configs.TestFile, config *configs.Config) (terraform.InputValues, tfdiags.Diagnostics) {
	variables := make(map[string]backend.UnparsedVariableValue)
	for key, variable := range config.Module.Variables {
		if concrete, ok := test.Variables[key]; ok {
			variables[key] = unparsedVariableValueExpression{
				expr:       concrete,
				sourceType: terraform.ValueFromConfig,
			}
			continue
		}

		if !variable.Required() {
			continue
		}
		variables[key] = unparsedUnknownVariableValue{
			Name:     key,
			WantType: variable.Type,
		}
	}
	return backend.ParseVariableValues(variables, config.Module.Variables)
}

func (c *MTestCommand) collateVariables(test *configs.TestFile, run *configs.Run, config *configs.Config) (terraform.InputValues, tfdiags.Diagnostics) {
	variables := make(map[string]backend.UnparsedVariableValue)
	for key, value := range test.Variables {
		variables[key] = unparsedVariableValueExpression{
			expr:       value,
			sourceType: terraform.ValueFromConfig,
		}
	}
	for key, value := range run.Variables {
		variables[key] = unparsedVariableValueExpression{
			expr:       value,
			sourceType: terraform.ValueFromConfig,
		}
	}
	return backend.ParseVariableValues(variables, config.Module.Variables)
}
