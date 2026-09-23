package copilot

import (
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"sort"
	"strings"
)

// SandboxDenialNote explains why denials can survive every permission option,
// for callers reporting recommendations of an evaluation that used --sandbox.
const SandboxDenialNote = "--sandbox enforces an OS-level filesystem policy on top of the Copilot CLI's tool permissions, so denials on paths no --add-dir can reach (symlinks out of the working directory, network access) persist until --sandbox is dropped"

// shellToolNames are the names the Copilot CLI displays for tool calls whose
// body is a shell command line.
var shellToolNames = map[string]bool{
	"shell":           true,
	"sandboxed shell": true,
}

// ungrantableDirs are directories not worth recommending: the Copilot CLI
// sandbox refuses to grant the system ones, and the roots holding every user's
// files only ever appear here because a path was recovered incompletely.
var ungrantableDirs = map[string]bool{
	"/":         true,
	"/Users":    true,
	"/bin":      true,
	"/boot":     true,
	"/dev":      true,
	"/home":     true,
	"/proc":     true,
	"/sbin":     true,
	"/sys":      true,
	"/usr":      true,
	"/usr/bin":  true,
	"/usr/sbin": true,
	"/var":      true,
}

// pathBypassOptions make --add-dir recommendations redundant.
var pathBypassOptions = []string{"--allow-all-paths", "--allow-all", "--yolo"}

// writeCommands are the commands whose every path argument is written to.
// Commands where only some arguments are a destination (cp, mv, sed -i) are
// left out on purpose: a missing write shows up as another denial, while an
// unneeded one stays in the user's settings for good.
var writeCommands = map[string]bool{
	"mkdir":    true,
	"rm":       true,
	"rmdir":    true,
	"tee":      true,
	"touch":    true,
	"truncate": true,
}

// commandNamePattern matches a plausible command name, so that a quoted string
// or an option leading a mis-split segment is not turned into a permission.
var commandNamePattern = regexp.MustCompile(`^[A-Za-z0-9_.][A-Za-z0-9_.-]*$`)

// recommendPermissions returns the Copilot CLI options that would have allowed
// the denied calls, most specific first, along with the directories the calls
// were going to write to. Options already in effect are omitted, and
// --allow-all-paths is never suggested: paths are narrowed to --add-dir so a
// re-run stays as restricted as the denials allow.
func recommendPermissions(opts EvaluateOptions, calls []deniedCall) (options []string, writable []string) {
	if len(calls) == 0 {
		return nil, nil
	}
	existing := existingOptions(opts)
	dirOptions, writable := recommendDirOptions(opts, calls, existing)
	return append(recommendToolOptions(calls, existing), dirOptions...), writable
}

// recommendToolOptions returns --allow-tool options for the commands the
// denied calls tried to run, falling back to --allow-all-tools for calls whose
// command could not be determined.
func recommendToolOptions(calls []deniedCall, existing map[string]bool) []string {
	if existing["--allow-all-tools"] {
		return nil
	}
	names := make([]string, 0, len(calls))
	fallback := false
	for _, c := range calls {
		commands := deniedCommands(c)
		if len(commands) == 0 {
			fallback = true
			continue
		}
		for _, name := range commands {
			if !slices.Contains(names, name) {
				names = append(names, name)
			}
		}
	}
	sort.Strings(names)
	options := make([]string, 0, len(names)+1)
	for _, name := range names {
		if option := "--allow-tool=shell(" + name + ":*)"; !existing[option] {
			options = append(options, option)
		}
	}
	if fallback {
		options = append(options, "--allow-all-tools")
	}
	return options
}

// deniedCommands returns the command names a denied call tried to run. The
// Copilot CLI displays a shell call either as "shell" or as the command name
// itself, so the body is read as a command line only when the display name
// says so or matches what the body starts with.
func deniedCommands(c deniedCall) []string {
	var names []string
	for _, segment := range splitShellSegments(joinBody(c.Body)) {
		if name, _ := splitCommand(segment); name != "" && !slices.Contains(names, name) {
			names = append(names, name)
		}
	}
	if len(names) == 0 {
		return nil
	}
	if !shellToolNames[strings.ToLower(c.Tool)] && !slices.Contains(names, c.Tool) {
		return nil
	}
	return names
}

// splitShellSegments splits a command line on the separators that start a new
// command. Quoting is ignored: a wrong split only costs an extra suggestion.
func splitShellSegments(line string) []string {
	return strings.FieldsFunc(line, func(r rune) bool {
		return r == ';' || r == '|' || r == '&' || r == '\n'
	})
}

// splitCommand returns the command a shell segment runs and its arguments,
// skipping leading VAR=value assignments. The name is "" when the segment does
// not start with a plausible command name.
func splitCommand(segment string) (string, []string) {
	fields := strings.Fields(segment)
	for i, field := range fields {
		if strings.ContainsRune(field, '=') && !strings.ContainsAny(field, `/\`) {
			continue
		}
		name := filepath.Base(field)
		if !commandNamePattern.MatchString(name) {
			return "", fields[i+1:]
		}
		return name, fields[i+1:]
	}
	return "", nil
}

// recommendDirOptions returns --add-dir options for the directories the denied
// calls touched, dropping any directory already covered by an ancestor or by
// the working directory the sandbox is granted, along with the subset the
// calls were definitely going to write to. Only arguments are considered: the
// sandbox already grants the directories on PATH that the commands themselves
// live in.
func recommendDirOptions(opts EvaluateOptions, calls []deniedCall, existing map[string]bool) (options []string, writable []string) {
	for _, option := range pathBypassOptions {
		if existing[option] {
			return nil, nil
		}
	}
	granted := sandboxWorkingDir(opts)
	var dirs []string
	for _, c := range calls {
		for _, segment := range splitShellSegments(joinBody(c.Body)) {
			name, args := splitCommand(segment)
			writes := writeCommands[name]
			pendingRedirect := false
			for _, arg := range args {
				token, redirected := splitRedirect(arg)
				if redirected && token == "" {
					pendingRedirect = true
					continue
				}
				redirected = redirected || pendingRedirect
				pendingRedirect = false
				dir := pathGrant(token)
				if dir == "" || covers(granted, dir) {
					continue
				}
				dirs = append(dirs, dir)
				if writes || redirected {
					writable = append(writable, dir)
				}
			}
		}
	}
	options = make([]string, 0, len(dirs))
	for _, dir := range collapseDirs(dirs) {
		if option := "--add-dir=" + dir; !existing[option] {
			options = append(options, option)
		}
	}
	return options, collapseDirs(writable)
}

// splitRedirect strips the shell output redirection an argument can lead with,
// reporting whether one was there. An empty result means the redirection
// target is the argument that follows.
func splitRedirect(arg string) (string, bool) {
	trimmed := strings.TrimLeft(arg, "0123456789&")
	if rest, ok := strings.CutPrefix(trimmed, ">>"); ok {
		return rest, true
	}
	if rest, ok := strings.CutPrefix(trimmed, ">"); ok {
		return strings.TrimPrefix(rest, "|"), true
	}
	return arg, false
}

// joinBody rebuilds the command line a denied call's body lines render. The
// Copilot CLI hard-wraps a long line mid-token, so lines are glued with no
// separator whenever that recovers an existing path; otherwise a fragment such
// as "/Users/takaz" would be read as a token of its own and widened to the
// directory that happens to exist above it.
func joinBody(body []string) string {
	var line string
	for i, next := range body {
		switch {
		case i == 0:
			line = next
		case continuesToken(line, next):
			line += next
		default:
			line += " " + next
		}
	}
	return line
}

// continuesToken reports whether next resumes the last token of line, which is
// assumed when gluing the two halves names an existing path.
func continuesToken(line, next string) bool {
	head := strings.Fields(line)
	tail := strings.Fields(next)
	if len(head) == 0 || len(tail) == 0 {
		return false
	}
	glued := normalizePathToken(head[len(head)-1] + tail[0])
	if glued == "" {
		return false
	}
	_, err := os.Stat(glued)
	return err == nil
}

// pathGrant turns a command-line token into an existing directory to grant
// access to, returning "" for tokens that are not absolute paths or that
// resolve to a directory the sandbox never grants anyway. The Copilot CLI
// fails at startup on an --add-dir that is not an existing directory.
func pathGrant(token string) string {
	path := normalizePathToken(token)
	if path == "" {
		return ""
	}
	if dir := nearestExistingDir(path); !ungrantableDirs[dir] {
		return dir
	}
	return ""
}

// normalizePathToken strips the punctuation a token can carry in a rendered
// command line and expands a leading ~, returning "" unless the result is an
// absolute path without glob metacharacters.
func normalizePathToken(token string) string {
	token = strings.Trim(token, "\"'`,;()")
	if after, ok := strings.CutPrefix(token, "~/"); ok {
		home, err := os.UserHomeDir()
		if err != nil {
			return ""
		}
		token = filepath.Join(home, after)
	}
	if !strings.HasPrefix(token, "/") || strings.ContainsAny(token, "*?[") {
		return ""
	}
	return filepath.Clean(token)
}

// nearestExistingDir returns path when it is a directory, otherwise its
// nearest existing ancestor directory, or "" when none exists below the root.
func nearestExistingDir(path string) string {
	for path != "/" && path != "." {
		info, err := os.Stat(path)
		switch {
		case err != nil:
			path = filepath.Dir(path)
		case info.IsDir():
			return path
		default:
			return filepath.Dir(path)
		}
	}
	return ""
}

// collapseDirs sorts directories and drops every one already covered by an
// ancestor in the list.
func collapseDirs(dirs []string) []string {
	sort.Strings(dirs)
	var kept []string
	for _, dir := range dirs {
		if len(kept) > 0 && covers(kept[len(kept)-1], dir) {
			continue
		}
		kept = append(kept, dir)
	}
	return kept
}

// covers reports whether granting parent also grants dir.
func covers(parent, dir string) bool {
	if parent == "" {
		return false
	}
	return dir == parent || strings.HasPrefix(dir, parent+string(filepath.Separator))
}

// existingOptions indexes the permission options an evaluation already used so
// that they are not recommended again, normalizing the "--opt value" form to
// the "--opt=value" form the recommendations use.
func existingOptions(opts EvaluateOptions) map[string]bool {
	existing := make(map[string]bool, len(opts.ExtraArgs)+1)
	if opts.AllowAllTools {
		existing["--allow-all-tools"] = true
	}
	for i, arg := range opts.ExtraArgs {
		existing[arg] = true
		if strings.HasPrefix(arg, "--") && !strings.Contains(arg, "=") && i+1 < len(opts.ExtraArgs) {
			existing[arg+"="+opts.ExtraArgs[i+1]] = true
		}
	}
	return existing
}

// SandboxSettingsHint returns the ~/.copilot/settings.json fragment that grants
// the directories recommended in options for every run, or "" when no
// directory was recommended. User settings are the only durable place for it:
// the Copilot CLI reads repository settings (.github/copilot/settings.json and
// settings.local.json) only in interactive mode, never in the -p mode used
// here. A directory is granted read-write only when writablePaths shows a
// denied call was going to write into it; read access is enough otherwise.
func SandboxSettingsHint(options []string, writablePaths []string) string {
	var readonly, readwrite []string
	for _, option := range options {
		dir, ok := strings.CutPrefix(option, "--add-dir=")
		if !ok {
			continue
		}
		if needsWrite(dir, writablePaths) {
			readwrite = append(readwrite, resolveSymlinks(dir))
			continue
		}
		readonly = append(readonly, resolveSymlinks(dir))
	}
	if len(readonly)+len(readwrite) == 0 {
		return ""
	}
	filesystem := make(map[string]any, 2)
	if len(readonly) > 0 {
		filesystem["readonlyPaths"] = collapseDirs(readonly)
	}
	if len(readwrite) > 0 {
		filesystem["readwritePaths"] = collapseDirs(readwrite)
	}
	fragment, err := json.Marshal(map[string]any{
		"sandbox": map[string]any{
			"userPolicy": map[string]any{"filesystem": filesystem},
		},
	})
	if err != nil {
		return ""
	}
	return string(fragment)
}

// needsWrite reports whether granting dir has to allow writes, which is so
// when dir holds a path a denied call was going to write to.
func needsWrite(dir string, writablePaths []string) bool {
	for _, path := range writablePaths {
		if covers(dir, path) {
			return true
		}
	}
	return false
}

// resolveSymlinks returns the real path of dir, which the OS-level sandbox
// policy matches against: on macOS /tmp is a link to /private/tmp, so a policy
// naming /tmp never applies. The path is returned untouched when it cannot be
// resolved, leaving a hint that is no worse than before.
func resolveSymlinks(dir string) string {
	resolved, err := filepath.EvalSymlinks(dir)
	if err != nil {
		return dir
	}
	return resolved
}

// FormatRecommendations renders recommended options as an argument list that
// can be pasted after a -- separator as is.
func FormatRecommendations(options []string) string {
	quoted := make([]string, 0, len(options))
	for _, option := range options {
		quoted = append(quoted, shellQuoteOption(option))
	}
	return strings.Join(quoted, " ")
}

// shellQuoteOption single-quotes an option's value when it contains shell
// metacharacters, e.g. the parentheses and glob in shell(go:*).
func shellQuoteOption(option string) string {
	name, value, ok := strings.Cut(option, "=")
	if !ok || !strings.ContainsAny(value, "*?()[]|&;<>$`\\ '\"") {
		return option
	}
	return name + "='" + strings.ReplaceAll(value, "'", `'\''`) + "'"
}
