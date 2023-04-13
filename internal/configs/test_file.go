package configs

import (
	"github.com/hashicorp/hcl/v2"
	"github.com/hashicorp/hcl/v2/gohcl"
)

// TestCommand represents the Terraform command a given run block will execute.
type TestCommand rune

type TestMode rune

const (
	// ApplyTestCommand causes the run block to execute a Terraform apply
	// operation.
	ApplyTestCommand TestCommand = 0

	// PlanTestCommand causes the run block to execute a Terraform plan
	// operation.
	PlanTestCommand TestCommand = 'P'

	// NormalTestMode causes the run block to execute in plans.NormalMode.
	NormalTestMode TestMode = 0

	// RefreshOnlyTestMode causes the run block to execute in
	// plans.RefreshOnlyMode.
	RefreshOnlyTestMode TestMode = 'R'
)

// TestFile represents a single test file within a `terraform test` execution.
//
// A test file is made up of a sequential list of run blocks, each designating
// a command to execute and a series of validations to check after the command.
type TestFile struct {
	// Variables defines a set of global variable definitions that should be set
	// for every run block within the test file.
	Variables []hcl.KeyValuePair

	// Runs defines the sequential list of run blocks that should be executed in
	// order.
	Runs []*Run
}

// Run represents a single run block within a test file.
//
// Each Run block represents a single Terraform command to be executed and a set
// of validations to run after the command.
type Run struct {
	// Module is the Terraform module that should be executed by this run block.
	//
	// By default, this will select the current module. If the selected module
	// isn't the current module then any CheckRules will only be able to
	// validate the outputs of the module due to scoping and visibility.
	Module string

	// Command is the Terraform command to execute.
	//
	// One of ['apply', 'plan'].
	Command TestCommand

	// Mode is the planning mode to run in. This field should not be set if the
	// Command is 'destroy'.
	//
	// One of ['normal', 'refresh-only'].
	Mode TestMode

	// Refresh is analogous to the -refresh=false Terraform plan option.
	Refresh bool

	// Replace is analogous to the -refresh=ADDRESS Terraform plan option.
	Replace []hcl.Traversal

	// Target is analogous to the -target=ADDRESS Terraform plan option.
	Target []hcl.Traversal

	// Variables defines a set of variable definitions for this command.
	//
	// Any variables specified locally that clash with the global variables will
	// take precedence over the global definition.
	Variables []hcl.KeyValuePair

	// CheckRules defines the list of assertions/validations that should be
	// checked by this run block.
	CheckRules []*CheckRule
}

func loadTestFile(body hcl.Body) (*TestFile, hcl.Diagnostics) {
	var diags hcl.Diagnostics

	content, contentDiags := body.Content(testFileSchema)
	diags = append(diags, contentDiags...)

	tf := TestFile{}

	for _, block := range content.Blocks {
		switch block.Type {
		case "run":
			run, runDiags := decodeTestRun(block)
			diags = append(diags, runDiags...)
			if !runDiags.HasErrors() {
				tf.Runs = append(tf.Runs, run)
			}
		default:
			continue
		}
	}

	if attr, exists := content.Attributes["variables"]; exists {
		vars, varsDiags := hcl.ExprMap(attr.Expr)
		diags = append(diags, varsDiags...)
		tf.Variables = vars
	}

	return &tf, diags
}

func decodeTestRun(block *hcl.Block) (*Run, hcl.Diagnostics) {
	var diags hcl.Diagnostics

	content, contentDiags := block.Body.Content(testRunSchema)
	diags = append(diags, contentDiags...)

	r := Run{}
	for _, block := range content.Blocks {
		switch block.Type {
		case "assert":
			cr, crDiags := decodeCheckRuleBlock(block, false)
			diags = append(diags, crDiags...)
			if !crDiags.HasErrors() {
				r.CheckRules = append(r.CheckRules, cr)
			}
		default:
			continue
		}
	}

	if attr, exists := content.Attributes["module"]; exists {
		diags = append(diags, gohcl.DecodeExpression(attr.Expr, nil, &r.Module)...)
	}

	if attr, exists := content.Attributes["command"]; exists {
		expr, exprDiags := shimTraversalInString(attr.Expr, true)
		diags = append(diags, exprDiags...)

		switch hcl.ExprAsKeyword(expr) {
		case "apply":
			r.Command = ApplyTestCommand
		case "plan":
			r.Command = PlanTestCommand
		default:
			diags = append(diags, &hcl.Diagnostic{
				Severity: hcl.DiagError,
				Summary:  "Invalid \"command\" keyword",
				Detail:   "The \"command\" argument requires one of the following keywords: apply or plan.",
				Subject:  attr.Expr.Range().Ptr(),
			})
		}
	} else {
		r.Command = ApplyTestCommand // Default to apply
	}

	if attr, exists := content.Attributes["mode"]; exists {
		expr, exprDiags := shimTraversalInString(attr.Expr, true)
		diags = append(diags, exprDiags...)

		switch hcl.ExprAsKeyword(expr) {
		case "refresh-only":
			r.Mode = RefreshOnlyTestMode
		case "normal":
			r.Mode = NormalTestMode
		default:
			diags = append(diags, &hcl.Diagnostic{
				Severity: hcl.DiagError,
				Summary:  "Invalid \"mode\" command",
				Detail:   "The \"mode\" argument requires one of the following keywords: normal or refresh-only",
				Subject:  attr.Expr.Range().Ptr(),
			})
		}
	} else {
		r.Mode = NormalTestMode // Default to normal
	}

	if attr, exists := content.Attributes["refresh"]; exists {
		diags = append(diags, gohcl.DecodeExpression(attr.Expr, nil, &r.Refresh)...)
	}

	if attr, exists := content.Attributes["replace"]; exists {
		reps, repsDiags := decodeDependsOn(attr)
		repsDiags = append(diags, repsDiags...)
		r.Replace = reps
	}

	if attr, exists := content.Attributes["target"]; exists {
		tars, tarsDiags := decodeDependsOn(attr)
		tarsDiags = append(diags, tarsDiags...)
		r.Target = tars
	}

	if attr, exists := content.Attributes["variables"]; exists {
		vars, varsDiags := hcl.ExprMap(attr.Expr)
		diags = append(diags, varsDiags...)
		r.Variables = vars
	}

	return &r, diags
}

var testFileSchema = &hcl.BodySchema{
	Attributes: []hcl.AttributeSchema{
		{Name: "variables"},
	},
	Blocks: []hcl.BlockHeaderSchema{
		{
			Type: "run",
		},
	},
}

var testRunSchema = &hcl.BodySchema{
	Attributes: []hcl.AttributeSchema{
		{Name: "module"},
		{Name: "command"},
		{Name: "mode"},
		{Name: "refresh"},
		{Name: "replace"},
		{Name: "target"},
		{Name: "variables"},
	},
	Blocks: []hcl.BlockHeaderSchema{
		{
			Type: "assert",
		},
	},
}
