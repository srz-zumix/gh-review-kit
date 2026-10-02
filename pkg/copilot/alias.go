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

// AliasFlagOptions controls how the flags set on a command are turned into alias flags.
type AliasFlagOptions struct {
	// Skip lists flags to leave out of the alias.
	Skip []string
	// PathFlags lists flags whose values are filesystem paths, made absolute so the alias works from any directory.
	PathFlags []string
	// ExecPathFlags lists flags whose values are an executable name or path: path-like values are made absolute,
	// while bare names are preserved for PATH lookup.
	ExecPathFlags []string
	// Rename maps deprecated flag names to the replacement to embed in the alias instead.
	Rename map[string]string
}

// CollectAliasFlags returns the flags set on fs, in order, normalized according to opts.
func CollectAliasFlags(fs *pflag.FlagSet, opts AliasFlagOptions) ([]AliasFlag, error) {
	var flags []AliasFlag
	index := map[string]int{}
	var err error
	fs.Visit(func(f *pflag.Flag) {
		if err != nil || slices.Contains(opts.Skip, f.Name) {
			return
		}
		values := []string{f.Value.String()}
		if sv, ok := f.Value.(pflag.SliceValue); ok {
			values = sv.GetSlice()
		}
		makeAbs := slices.Contains(opts.PathFlags, f.Name)
		execPath := slices.Contains(opts.ExecPathFlags, f.Name)
		for i, v := range values {
			if !makeAbs && !(execPath && isPathLike(v)) {
				continue
			}
			if values[i], err = filepath.Abs(v); err != nil {
				err = fmt.Errorf("failed to resolve path of --%s '%s': %w", f.Name, v, err)
				return
			}
		}
		name := f.Name
		if replacement, ok := opts.Rename[name]; ok {
			name = replacement
		}
		// A deprecated flag and its replacement resolve to the same name; the last one given wins.
		if i, ok := index[name]; ok {
			flags[i].Values = values
			return
		}
		index[name] = len(flags)
		flags = append(flags, AliasFlag{Name: name, Values: values})
	})
	return flags, err
}

// isPathLike reports whether v refers to an executable by path rather than by a bare name resolved through PATH.
func isPathLike(v string) bool {
	return strings.ContainsRune(v, '/') || strings.ContainsRune(v, filepath.Separator)
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
