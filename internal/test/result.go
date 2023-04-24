package test

import (
	"github.com/hashicorp/terraform/internal/plans"
	"github.com/hashicorp/terraform/internal/tfdiags"
)

type Run struct {
	Errors   tfdiags.Diagnostics
	Failures tfdiags.Diagnostics
	Warnings tfdiags.Diagnostics

	Plan *plans.Plan
}

type File struct {
	Path string
	Runs []Run

	TotalCount   int
	SkippedCount int
	ErrorCount   int
	FailureCount int
}

type Files struct {
	Files []File

	TotalCount   int
	SkippedCount int
	ErrorCount   int
	FailureCount int
}
