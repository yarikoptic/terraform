package views

import (
	"fmt"
	"github.com/mitchellh/colorstring"

	"github.com/hashicorp/terraform/internal/terminal"
	"github.com/hashicorp/terraform/internal/test"
	"github.com/hashicorp/terraform/internal/tfdiags"
)

type MTest interface {
	Start(path string)
	Complete(path string, file test.File)
	Summary(files test.Files)

	Diagnostics(tfdiags.Diagnostics)
}

func NewMTest(base *View) MTest {
	return &mTestHuman{
		streams:         base.streams,
		showDiagnostics: base.Diagnostics,
		colorize:        base.colorize,
	}
}

type mTestHuman struct {
	// This is the subset of functionality we need from the base view.
	streams         *terminal.Streams
	showDiagnostics func(diags tfdiags.Diagnostics)
	colorize        *colorstring.Colorize
}

func (v mTestHuman) Start(path string) {
	v.streams.Printf("-- %s... ", path)
}

func (v mTestHuman) Complete(path string, file test.File) {
	if file.ErrorCount > 0 || file.FailureCount > 0 {
		v.streams.Println(v.colorize.Color("[red]fail"))
	} else if file.SkippedCount > 0 {
		v.streams.Println(v.colorize.Color(fmt.Sprintf("[green]pass[reset], %d skipped", file.SkippedCount)))
	} else {
		v.streams.Println(v.colorize.Color("[green]pass"))
	}

	for _, run := range file.Runs {
		v.showDiagnostics(run.Failures)
		v.showDiagnostics(run.Errors)
	}
}

func (v mTestHuman) Summary(files test.Files) {
	if len(files.Files) == 0 {
		v.streams.Print(v.colorize.Color("[bold][yellow]No tests defined.[reset] This module doesn't have any test files to run.\n\n"))
		return
	}

	passed := files.TotalCount - (files.FailureCount + files.ErrorCount + files.SkippedCount)

	var skipped, failed, errored string
	if files.SkippedCount > 0 {
		skipped = fmt.Sprintf(", and skipped %d)", files.SkippedCount)
	}

	if files.FailureCount == 0 && files.ErrorCount == 0 {
		if files.TotalCount > 0 {
			v.streams.Println()
		}
		v.streams.Print(v.colorize.Color("[bold][green]Success![reset] "))
		v.streams.Printf(" %d test assertion(s) passed%s.\n\n", passed, skipped)
		return
	}

	if files.FailureCount > 0 {
		failed = fmt.Sprintf(", %d test assertion(s) failed", files.FailureCount)
	}
	if files.ErrorCount > 0 {
		errored = fmt.Sprintf(", %d test assertion(s) errored", files.ErrorCount)
	}

	v.streams.Print(v.colorize.Color("[bold][red]Failed! "))
	v.streams.Printf("%d test assertion(s) passed%s%s%s.\n\n", passed, failed, errored, skipped)
}

func (v mTestHuman) Diagnostics(diagnostics tfdiags.Diagnostics) {
	v.showDiagnostics(diagnostics)
}
