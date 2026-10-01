package copilot

import (
	"context"
	"fmt"
	"io"
	"os/exec"
	"path/filepath"
	"regexp"
	"slices"
	"strings"

	"github.com/spf13/pflag"
)

// AliasFlag is a command-line flag to embed in a generated gh alias; every value is emitted as its own --name=value.
type AliasFlag struct {
	Name   string
	Values []string
}

var shellSafe = regexp.MustCompile(`^[A-Za-z0-9_@%+=:,./-]+$`)

// shellQuote quotes s as a single POSIX shell word.
func shellQuote(s string) string {
	if shellSafe.MatchString(s) {
		return s
	}
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}

// CollectAliasFlags returns the flags set on fs, in order, except those listed in skip.
// Values of flags listed in pathFlags are made absolute so the alias works from any directory.
func CollectAliasFlags(fs *pflag.FlagSet, skip, pathFlags []string) ([]AliasFlag, error) {
	var flags []AliasFlag
	var err error
	fs.Visit(func(f *pflag.Flag) {
		if err != nil || slices.Contains(skip, f.Name) {
			return
		}
		values := []string{f.Value.String()}
		if sv, ok := f.Value.(pflag.SliceValue); ok {
			values = sv.GetSlice()
		}
		if slices.Contains(pathFlags, f.Name) {
			for i, v := range values {
				if values[i], err = filepath.Abs(v); err != nil {
					err = fmt.Errorf("failed to resolve path of --%s '%s': %w", f.Name, v, err)
					return
				}
			}
		}
		flags = append(flags, AliasFlag{Name: f.Name, Values: values})
	})
	return flags, err
}

// BuildAliasExpansion builds the shell expansion of a gh alias that runs "copilot comments" with the given flags.
// The alias takes an optional leading evaluator name (defaulting to defaultEvaluator) and forwards any further
// arguments, so they can add to or override the embedded flags. passthrough is forwarded to the evaluator CLI.
func BuildAliasExpansion(defaultEvaluator Evaluator, flags []AliasFlag, passthrough []string) string {
	var b strings.Builder
	fmt.Fprintf(&b, `evaluator=%s; case "$1" in %s) evaluator="$1"; shift;; esac; `,
		defaultEvaluator, strings.Join(Evaluators, "|"))
	b.WriteString(`gh review-kit copilot comments --evaluate="$evaluator"`)
	for _, f := range flags {
		for _, v := range f.Values {
			b.WriteString(" " + shellQuote("--"+f.Name+"="+v))
		}
	}
	b.WriteString(` "$@"`)
	if len(passthrough) > 0 {
		b.WriteString(" --")
		for _, a := range passthrough {
			b.WriteString(" " + shellQuote(a))
		}
	}
	return b.String()
}

// RegisterAlias registers expansion as the gh shell alias name, replacing an existing alias of that name.
func RegisterAlias(ctx context.Context, name, expansion string, out io.Writer) error {
	cmd := exec.CommandContext(ctx, "gh", "alias", "set", "--clobber", "--shell", name, expansion)
	cmd.Stdout = out
	cmd.Stderr = out
	return cmd.Run()
}
