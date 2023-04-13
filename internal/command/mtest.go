package command

import (
	"github.com/hashicorp/terraform/internal/addrs"
	"github.com/hashicorp/terraform/internal/backend"
	"github.com/hashicorp/terraform/internal/command/arguments"
	"github.com/hashicorp/terraform/internal/configs"
	"github.com/hashicorp/terraform/internal/plans"
	"github.com/hashicorp/terraform/internal/states"
	"github.com/hashicorp/terraform/internal/terraform"
)

type MTestCommand struct {
	Meta
}

func (c *MTestCommand) Run(rawArgs []string) int {
	common, rawArgs := arguments.ParseView(rawArgs)
	c.View.Configure(common)

	ctx, cancel := c.InterruptibleContext()
	defer cancel()

	loader, err := c.initConfigLoader()
	if err != nil {
		panic(err)
	}

	config, err := loader.LoadConfigWithTests(".")
	if err != nil {
		panic(err)
	}

	for _, test := range config.Module.Tests {
		state := states.NewState()

		for _, run := range test.Runs {
			opts, err := c.contextOpts()
			if err != nil {
				panic(err)
			}

			ctx, ctxDiags := terraform.NewContext(opts)
			if ctxDiags.HasErrors() {
				panic(ctxDiags.ErrWithWarnings())
			}

			var targets []addrs.Targetable
			for _, target := range run.Target {
				addr, addrDiags := addrs.ParseTarget(target)
				if addrDiags.HasErrors() {
					panic(addrDiags.ErrWithWarnings())
				}
				targets = append(targets, addr.Subject)
			}

			var replace []addrs.AbsResourceInstance
			for _, target := range run.Replace {
				addr, addrDiags := addrs.ParseAbsResourceInstance(target)
				if addrDiags.HasErrors() {
					panic(addrDiags.ErrWithWarnings())
				}

				if addr.Resource.Resource.Mode != addrs.ManagedResourceMode {
					panic("arguments/extended.go:140")
				}

				replace = append(replace, addr)
			}

			plan, planDiags := ctx.Plan(config, state, &terraform.PlanOpts{
				Mode: func() plans.Mode {
					switch run.Mode {
					case configs.RefreshOnlyTestMode:
						return plans.RefreshOnlyMode
					default:
						return plans.NormalMode
					}
				}(),
				SetVariables: c.collateVariables(test, run, config),
				Targets:      targets,
				ForceReplace: replace,
			})

		}
	}

	return 0
}

func (c *MTestCommand) collateVariables(test *configs.TestFile, run *configs.Run, config *configs.Config) terraform.InputValues {
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
	vars, varDiags := backend.ParseVariableValues(variables, config.Module.Variables)
	if varDiags.HasErrors() {
		panic(varDiags.ErrWithWarnings())
	}
	return vars
}
