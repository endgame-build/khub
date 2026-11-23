// doctor.go is `khub doctor`: the self-diagnosis behind every other command.
// The checks live in internal/doctor; this file renders them.

package cli

import (
	"fmt"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"

	"github.com/endgame-build/khub/internal/doctor"
	"github.com/endgame-build/khub/internal/omap"
)

func registerDoctor(root *cobra.Command) {
	var format string
	cmd := newCmd("doctor", "Diagnose the workspace: git, schema, provenance, lock, storage layout.",
		"", func(cmd *cobra.Command, args []string) error {
			return Guard(format, func() error { return doctorRun(format) })
		})
	cmd.Args = clickArity(0)
	cmd.Flags().StringVar(&format, "format", "text", "text (on a TTY) or json.")
	root.AddCommand(cmd)
}

// doctorRun never refuses for the workspace's sake: a missing workspace is a
// failed check like any other, exit 1, so the call that asks what is wrong is
// never the one that breaks.
func doctorRun(format string) error {
	ws, rootErr := resolveRoot()
	report := doctor.Run(ws, rootErr)

	rows := make([]any, 0, len(report.Checks))
	for _, c := range report.Checks {
		code, message := doctorFinding(c)
		row := omap.New()
		row.Set("name", c.Name)
		row.Set("status", c.Status)
		if code == "" {
			row.Set("code", nil)
		} else {
			row.Set("code", code)
		}
		row.Set("message", message)
		rows = append(rows, row)
	}
	data := omap.New()
	if report.Root == "" {
		data.Set("workspace", nil)
	} else {
		data.Set("workspace", filepath.ToSlash(report.Root))
	}
	data.Set("passed", report.Passed())
	data.Set("checks", rows)

	if err := Emit(data, format, func() { doctorHuman(report, rows) }); err != nil {
		return err
	}
	if !report.Passed() {
		return &ExitError{Code: 1}
	}
	return nil
}

// doctorFinding is a check's code and message. An error the CLI recognises
// renders as every command renders it, after the check's own context; a
// Located error keeps its code.
func doctorFinding(c doctor.Check) (code, message string) {
	code, message = c.Code, c.Message
	if c.Err == nil {
		return code, message
	}
	errCode, errMessage, ok := describeErr(c.Err)
	if !ok {
		errMessage = c.Err.Error()
	}
	if errCode != "" {
		code = errCode
	}
	if message == "" {
		return code, errMessage
	}
	return code, message + ": " + errMessage
}

func doctorHuman(report *doctor.Report, rows []any) {
	width := 0
	for _, c := range report.Checks {
		width = max(width, len(c.Name))
	}
	failed, warned := 0, 0
	for i, c := range report.Checks {
		switch c.Status {
		case doctor.Fail:
			failed++
		case doctor.Warn:
			warned++
		}
		message, _ := rows[i].(*omap.Map).Get("message")
		// One line per check: a YAML parse error carries a multi-line
		// pointer at the fault, which the JSON keeps whole.
		line, _, _ := strings.Cut(message.(string), "\n")
		fmt.Printf("%-4s  %-*s  %s\n", c.Status, width, c.Name, line)
	}
	var tail []string
	if warned > 0 {
		tail = append(tail, countOf(warned, "warning"))
	}
	switch {
	case failed > 0:
		fmt.Printf("Doctor failed: %s\n", strings.Join(append([]string{countOf(failed, "failed check")}, tail...), ", "))
	case warned > 0:
		fmt.Printf("Doctor passed with %s\n", tail[0])
	default:
		fmt.Println("Doctor passed")
	}
}

func countOf(n int, word string) string {
	if n == 1 {
		return "1 " + word
	}
	return fmt.Sprintf("%d %ss", n, word)
}
