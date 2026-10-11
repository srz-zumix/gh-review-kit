package script

import (
	"bytes"
	"fmt"
	"strings"

	"mvdan.cc/sh/v3/syntax"
)

func literalWord(word *syntax.Word) (string, bool) {
	var text strings.Builder
	var appendParts func([]syntax.WordPart) bool
	appendParts = func(parts []syntax.WordPart) bool {
		for _, part := range parts {
			switch value := part.(type) {
			case *syntax.Lit:
				text.WriteString(value.Value)
			case *syntax.SglQuoted:
				if value.Dollar {
					return false
				}
				text.WriteString(value.Value)
			case *syntax.DblQuoted:
				if !appendParts(value.Parts) {
					return false
				}
			default:
				return false
			}
		}
		return true
	}
	known := appendParts(word.Parts)
	return text.String(), known
}

func wordText(word *syntax.Word) string {
	var output bytes.Buffer
	_ = syntax.NewPrinter().Print(&output, word)
	return output.String()
}

func Analyze(content []byte) ([]Finding, []Download) {
	return analyze(content, 0)
}

func analyze(content []byte, recursion int) ([]Finding, []Download) {
	var findings []Finding
	var downloads []Download
	if recursion > 3 {
		return []Finding{{Severity: "high", RuleID: "embedded-limit", Message: "Embedded shell nesting exceeds analysis limit"}}, nil
	}
	file, err := syntax.NewParser(syntax.Variant(syntax.LangBash)).Parse(bytes.NewReader(content), "script")
	if err != nil {
		return []Finding{{Severity: "high", RuleID: "parse-error", Message: "Shell syntax could not be analyzed: " + err.Error()}}, nil
	}
	add := func(line int, risk Risk, rule, message string) {
		findings = append(findings, Finding{Line: line, Severity: risk, RuleID: rule, Message: message})
	}
	var calls []*syntax.CallExpr
	var executed = map[string]bool{}
	piped := map[*syntax.CallExpr]bool{}
	syntax.Walk(file, func(node syntax.Node) bool {
		binary, ok := node.(*syntax.BinaryCmd)
		if !ok || binary.Op != syntax.Pipe && binary.Op != syntax.PipeAll {
			return true
		}
		hasShell := false
		syntax.Walk(binary.Y, func(child syntax.Node) bool {
			if call, ok := child.(*syntax.CallExpr); ok && len(call.Args) > 0 {
				name, static := literalWord(call.Args[0])
				hasShell = hasShell || static && isShell(name)
			}
			return true
		})
		if hasShell {
			syntax.Walk(binary.X, func(child syntax.Node) bool {
				if call, ok := child.(*syntax.CallExpr); ok {
					piped[call] = true
				}
				return true
			})
		}
		return true
	})
	syntax.Walk(file, func(node syntax.Node) bool {
		call, ok := node.(*syntax.CallExpr)
		if !ok || len(call.Args) == 0 {
			return true
		}
		calls = append(calls, call)
		command, known := literalWord(call.Args[0])
		if known {
			executed[command] = true
		}
		if known && isShell(command) {
			for _, argument := range call.Args[1:] {
				if value, static := literalWord(argument); static {
					executed[value] = true
				}
			}
		}
		return true
	})
	for _, call := range calls {
		line := int(call.Pos().Line())
		command, known := literalWord(call.Args[0])
		if !known {
			add(line, "medium", "dynamic-command", "Computed command cannot be resolved statically")
			continue
		}
		command = command[strings.LastIndex(command, "/")+1:]
		var args []string
		for _, word := range call.Args[1:] {
			args = append(args, wordText(word))
		}
		text := strings.Join(args, " ")
		switch command {
		case "sudo", "doas", "su":
			add(line, "high", "privilege", "Requests elevated privileges")
		case "env", "exec", "alias":
			add(line, "medium", "command-wrapper", "May execute or redefine commands beyond static resolution")
		case "command":
			if !commandProbe(call.Args[1:]) {
				add(line, "medium", "command-wrapper", "May execute or redefine commands beyond static resolution")
			}
		case "dd", "fdisk", "diskutil", "mkfs", "mkfs.ext4", "mkfs.xfs":
			add(line, "critical", "disk-operation", "May overwrite disks or filesystem data")
		case "rm":
			add(line, "medium", "remove", "Removes files; inspect targets and variable expansion")
			recursive := strings.Contains(text, "--recursive")
			for _, arg := range args {
				if strings.HasPrefix(arg, "-") && !strings.HasPrefix(arg, "--") && strings.ContainsAny(arg, "rR") {
					recursive = true
				}
			}
			if recursive {
				add(line, "high", "recursive-remove", "Recursively removes files")
				for _, word := range call.Args[1:] {
					value, static := literalWord(word)
					if (static && (value == "/" || value == "/*")) || strings.Contains(wordText(word), "$HOME") || strings.Contains(wordText(word), "${HOME}") {
						add(line, "critical", "destructive-remove", "Recursive removal targets root or home")
					}
				}
			}
		case "eval":
			add(line, "high", "eval", "Executes computed shell code")
		case "python", "python3", "perl", "ruby", "node", "osascript":
			add(line, "high", "other-interpreter", "Executes code outside shell analysis")
		case "printf":
			if strings.Contains(text, `\x`) || strings.Contains(text, `\0`) {
				add(line, "medium", "encoded-content", "Escape sequences may conceal executable code")
			}
		case "base64", "openssl":
			add(line, "medium", "encoded-content", "Encoded content may conceal executable code")
		case "chmod":
			if strings.Contains(text, "777") || strings.Contains(text, "a+w") {
				add(line, "high", "permissions", "Makes files writable by other users")
			}
		case "crontab", "launchctl", "systemctl":
			add(line, "high", "persistence", "May install persistent tasks or services")
		case "nc", "ncat", "netcat", "socat":
			add(line, "high", "network-tool", "Opens network connections or listeners")
		case "xattr", "spctl", "setenforce":
			add(line, "high", "security-controls", "May change operating system security controls")
		case "history":
			if strings.Contains(text, "-c") {
				add(line, "high", "history-clear", "Clears shell history")
			}
		case "source", ".":
			add(line, "medium", "source", "Loads additional code not covered by this review")
		case "curl", "wget":
			found := false
			for _, word := range call.Args[1:] {
				value, static := literalWord(word)
				if static && (strings.HasPrefix(value, "https://") || strings.HasPrefix(value, "http://")) {
					found = true
					isScript := piped[call] || strings.Contains(value, ".sh") || !strings.Contains(text, "-o ") && !strings.Contains(text, "-O ")
					if !piped[call] {
						for _, suffix := range []string{".tar.gz", ".tgz", ".zip", ".xz", ".deb", ".rpm", ".dmg", ".pkg", ".exe", ".AppImage"} {
							if strings.HasSuffix(strings.SplitN(value, "?", 2)[0], suffix) {
								isScript = false
							}
						}
					}
					for index, arg := range args {
						if (arg == "-o" || arg == "-O" || arg == "--output") && index+2 < len(call.Args) {
							name, ok := literalWord(call.Args[index+2])
							isScript = isScript || ok && executed[name]
						}
					}
					downloads = append(downloads, Download{Line: line, URL: value, Script: isScript})
				} else if !static {
					if strings.Contains(wordText(word), "://") {
						downloads = append(downloads, Download{Line: line, URL: wordText(word), Script: true})
					}
					add(line, "medium", "unresolved-url", "Download argument uses expansion; destination cannot be resolved")
				}
			}
			if !found {
				add(line, "medium", "unresolved-download", "Download destination is unknown")
			}
			for _, sensitive := range []string{"TOKEN", "SECRET", "PASSWORD", "$(env", "`env"} {
				if strings.Contains(text, sensitive) {
					add(line, "high", "credential-transfer", "Network request references environment or credential data")
					break
				}
			}
			add(line, "medium", "runtime-download", "Runtime downloads are not pinned to the reviewed bytes; binaries and archives are not inspected")
		}
		if isShell(command) {
			add(line, "medium", "shell-execution", "Invokes another shell or executes additional code")
			for index, arg := range call.Args[1:] {
				value, static := literalWord(arg)
				if static && value == "-c" && index+2 < len(call.Args) {
					body, ok := literalWord(call.Args[index+2])
					if ok {
						inner, remote := analyze([]byte(body), recursion+1)
						for _, finding := range inner {
							finding.Line = line
							findings = append(findings, finding)
						}
						for _, download := range remote {
							download.Line = line
							downloads = append(downloads, download)
						}
					}
				}
			}
		}
		for _, target := range []string{".bashrc", ".zshrc", ".profile", "authorized_keys", "/dev/tcp", "/etc/"} {
			if strings.Contains(text, target) {
				risk := Risk("high")
				message := "References sensitive path " + target
				if target == "/etc/" {
					risk = etcAccessRisk(command, call.Args[1:])
					switch risk {
					case "low":
						message = "Reads or mentions system configuration path /etc/"
					case "high":
						message = "May modify system configuration path /etc/"
					default:
						message = "References /etc/ with unresolved access behavior"
					}
				}
				add(line, risk, "sensitive-path", message)
			}
		}
	}
	syntax.Walk(file, func(node syntax.Node) bool {
		if redirect, ok := node.(*syntax.Redirect); ok && redirect.Word != nil {
			text := wordText(redirect.Word)
			for _, target := range []string{".bashrc", ".zshrc", ".profile", "authorized_keys", "/dev/tcp", "/etc/"} {
				if strings.Contains(text, target) {
					if target == "/etc/" && redirect.Op == syntax.RdrIn {
						add(int(redirect.Pos().Line()), "low", "sensitive-path", "Reads system configuration path /etc/")
					} else {
						add(int(redirect.Pos().Line()), "high", "sensitive-write", fmt.Sprintf("References sensitive redirection target %s", target))
					}
				}
			}
		}
		return true
	})
	return findings, downloads
}

func commandProbe(args []*syntax.Word) bool {
	probe := false
	for _, word := range args {
		value, known := literalWord(word)
		if !known || !strings.HasPrefix(value, "-") || value == "--" {
			break
		}
		switch value {
		case "-p":
		case "-v", "-V", "-pv", "-pV", "-vp", "-Vp":
			probe = true
		default:
			return false
		}
	}
	return probe
}

func etcAccessRisk(command string, args []*syntax.Word) Risk {
	switch command {
	case "cat", "head", "tail", "grep", "egrep", "fgrep", "ls", "stat", "wc", "file", "readlink", "realpath", "test", "[", "echo", "printf":
		return "low"
	case "command":
		if commandProbe(args) {
			return "low"
		}
	case "sed":
		for _, word := range args {
			value, known := literalWord(word)
			if !known {
				return "medium"
			}
			if strings.HasPrefix(value, "--in-place") || strings.HasPrefix(value, "-") && !strings.HasPrefix(value, "--") && strings.Contains(value, "i") {
				return "high"
			}
		}
		return "medium"
	case "tee", "mv", "touch", "mkdir", "rm", "rmdir", "chmod", "chown", "chgrp", "truncate", "dd":
		return "high"
	case "cp", "install", "ln":
		if len(args) > 0 {
			target, static := literalWord(args[len(args)-1])
			if static && strings.Contains(target, "/etc/") {
				return "high"
			}
		}
		unresolved := false
		for index, word := range args {
			value, known := literalWord(word)
			if !known {
				unresolved = true
				continue
			}
			if strings.HasPrefix(value, "--target-directory=/etc/") || strings.HasPrefix(value, "-t/etc/") {
				return "high"
			}
			if (value == "-t" || value == "--target-directory") && index+1 < len(args) {
				target, static := literalWord(args[index+1])
				if !static {
					return "medium"
				}
				if strings.Contains(target, "/etc/") {
					return "high"
				}
			}
		}
		if unresolved {
			return "medium"
		}
		if len(args) > 0 {
			target, _ := literalWord(args[len(args)-1])
			if strings.Contains(target, "/etc/") {
				return "high"
			}
			return "low"
		}
	}
	return "medium"
}

func isShell(command string) bool {
	command = command[strings.LastIndex(command, "/")+1:]
	switch command {
	case "sh", "bash", "dash", "zsh", "ksh":
		return true
	}
	return false
}
