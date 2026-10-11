package script

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"
)

type Risk string

var Risks = []Risk{"none", "low", "medium", "high", "critical"}

func ParseRisk(value string) (Risk, error) {
	for _, risk := range Risks {
		if string(risk) == value {
			return risk, nil
		}
	}
	return "", fmt.Errorf("invalid risk %q", value)
}

func rank(risk Risk) int {
	for index, value := range Risks {
		if value == risk {
			return index
		}
	}
	return len(Risks)
}

type Finding struct {
	Line     int    `json:"line"`
	Severity Risk   `json:"severity"`
	RuleID   string `json:"rule_id"`
	Message  string `json:"message"`
}

type Download struct {
	Line   int    `json:"line"`
	URL    string `json:"url"`
	Script bool   `json:"script"`
}

type Node struct {
	Source    string     `json:"source"`
	SHA256    string     `json:"sha256"`
	Risk      Risk       `json:"risk"`
	Findings  []Finding  `json:"findings"`
	Downloads []Download `json:"downloads"`
	AI        *AIResult  `json:"ai,omitempty"`
	Children  []*Node    `json:"children,omitempty"`
	Content   []byte     `json:"-"`
}

func (node *Node) add(finding Finding) {
	node.Findings = append(node.Findings, finding)
	if rank(finding.Severity) > rank(node.Risk) {
		node.Risk = finding.Severity
	}
}

type Options struct {
	AllowHTTP    bool
	MaxSize      int64
	Depth        int
	SHA256       string
	ChecksumsURL string
	Timeout      time.Duration
	Evaluator    string
	Bin          string
	Model        string
	Language     string
	PromptFile   string
	Prompt       string
	WorkDir      string
}

func DefaultOptions() Options {
	return Options{MaxSize: 5 << 20, Depth: 1, Timeout: 2 * time.Minute}
}

func ValidateOptions(opts Options) error {
	if opts.MaxSize <= 0 || opts.MaxSize > 64<<20 {
		return fmt.Errorf("max-size must be between 1 and 67108864 bytes")
	}
	if opts.Depth < 0 || opts.Depth > 3 {
		return fmt.Errorf("depth must be between 0 and 3")
	}
	if opts.Timeout <= 0 {
		return fmt.Errorf("timeout must be positive")
	}
	if opts.SHA256 != "" {
		digest, err := hex.DecodeString(opts.SHA256)
		if err != nil || len(digest) != sha256.Size {
			return fmt.Errorf("sha256 must contain exactly 64 hexadecimal characters")
		}
	}
	if opts.SHA256 != "" && opts.ChecksumsURL != "" {
		return fmt.Errorf("sha256 and checksums-url are mutually exclusive")
	}
	if opts.Evaluator != "" && opts.Evaluator != "copilot" && opts.Evaluator != "claude" {
		return fmt.Errorf("unsupported evaluator %q", opts.Evaluator)
	}
	if opts.Evaluator == "" && (opts.PromptFile != "" || opts.Bin != "" || opts.Model != "") {
		return fmt.Errorf("AI options require --evaluate")
	}
	return nil
}

func digest(content []byte) string {
	sum := sha256.Sum256(content)
	return hex.EncodeToString(sum[:])
}

func readLimited(reader io.Reader, limit int64) ([]byte, error) {
	content, err := io.ReadAll(io.LimitReader(reader, limit+1))
	if err != nil {
		return nil, err
	}
	if int64(len(content)) > limit {
		return nil, fmt.Errorf("content exceeds %d bytes", limit)
	}
	return content, nil
}

func checkURL(raw string, allowHTTP bool) error {
	parsed, err := url.Parse(raw)
	if err != nil || parsed.Hostname() == "" || parsed.User != nil {
		return fmt.Errorf("invalid download URL")
	}
	if parsed.Scheme != "https" && !(allowHTTP && parsed.Scheme == "http") {
		return fmt.Errorf("HTTPS is required (use --allow-http to permit HTTP)")
	}
	return nil
}

func fetch(ctx context.Context, raw string, opts Options) ([]byte, string, error) {
	if err := checkURL(raw, opts.AllowHTTP); err != nil {
		return nil, "", err
	}
	client := &http.Client{Timeout: opts.Timeout, CheckRedirect: func(req *http.Request, via []*http.Request) error {
		if len(via) >= 5 {
			return fmt.Errorf("too many redirects")
		}
		return checkURL(req.URL.String(), opts.AllowHTTP)
	}}
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, raw, nil)
	if err != nil {
		return nil, "", err
	}
	response, err := client.Do(request)
	if err != nil {
		return nil, "", err
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return nil, "", fmt.Errorf("download returned HTTP %d", response.StatusCode)
	}
	content, err := readLimited(response.Body, opts.MaxSize)
	return content, response.Header.Get("Content-Type"), err
}

func Review(ctx context.Context, source string, stdin io.Reader, opts Options) (*Node, error) {
	if err := ValidateOptions(opts); err != nil {
		return nil, err
	}
	if opts.Evaluator != "" && opts.WorkDir == "" {
		directory, err := os.MkdirTemp("", "gh-review-kit-script-ai-")
		if err != nil {
			return nil, err
		}
		defer os.RemoveAll(directory)
		opts.WorkDir = directory
	}
	if opts.PromptFile != "" {
		prompt, err := os.ReadFile(opts.PromptFile)
		if err != nil {
			return nil, fmt.Errorf("read review prompt: %w", err)
		}
		opts.Prompt = string(prompt)
	}
	var content []byte
	var contentType string
	var err error
	switch {
	case source == "-":
		content, err = readLimited(stdin, opts.MaxSize)
	case strings.Contains(source, "://"):
		content, contentType, err = fetch(ctx, source, opts)
	default:
		var file *os.File
		file, err = os.Open(source)
		if err == nil {
			defer file.Close()
			content, err = readLimited(file, opts.MaxSize)
		}
	}
	if err != nil {
		return nil, fmt.Errorf("read script: %w", err)
	}
	if opts.SHA256 != "" && !strings.EqualFold(opts.SHA256, digest(content)) {
		return nil, fmt.Errorf("script SHA256 mismatch")
	}
	if opts.ChecksumsURL != "" {
		if err := verifyChecksums(ctx, source, content, opts); err != nil {
			return nil, err
		}
	}
	seen := map[string]bool{source: true}
	count := 1
	node, err := reviewContent(ctx, source, content, opts, 0, seen, &count)
	if err != nil {
		return nil, err
	}
	if strings.Contains(strings.ToLower(contentType), "text/html") {
		node.add(Finding{Severity: "high", RuleID: "html-response", Message: "Server returned HTML instead of a shell script"})
	}
	return node, nil
}

func verifyChecksums(ctx context.Context, source string, content []byte, opts Options) error {
	parsed, err := url.Parse(source)
	if err != nil || source == "-" {
		return fmt.Errorf("checksums-url requires a named source")
	}
	name := parsed.Path[strings.LastIndex(parsed.Path, "/")+1:]
	if name == "" {
		return fmt.Errorf("source has no checksum filename")
	}
	sums, _, err := fetch(ctx, opts.ChecksumsURL, opts)
	if err != nil {
		return fmt.Errorf("fetch checksums: %w", err)
	}
	matches := 0
	for _, line := range strings.Split(string(sums), "\n") {
		fields := strings.Fields(line)
		if len(fields) == 2 && strings.TrimPrefix(fields[1], "*") == name {
			matches++
			if !strings.EqualFold(fields[0], digest(content)) {
				return fmt.Errorf("script SHA256 mismatch")
			}
		}
	}
	if matches != 1 {
		return fmt.Errorf("checksum file must contain exactly one entry for %q", name)
	}
	return nil
}

func reviewContent(ctx context.Context, source string, content []byte, opts Options, depth int, seen map[string]bool, count *int) (*Node, error) {
	node := &Node{Source: source, SHA256: digest(content), Risk: "none", Content: content}
	findings, downloads := Analyze(content)
	node.Downloads = downloads
	for _, finding := range findings {
		node.add(finding)
	}
	for _, download := range downloads {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if !download.Script || !strings.HasPrefix(download.URL, "http") || strings.ContainsAny(download.URL, "$`") {
			continue
		}
		if depth >= opts.Depth || *count >= 16 || seen[download.URL] {
			node.add(Finding{Line: download.Line, Severity: "medium", RuleID: "review-limit", Message: "Nested script not reviewed: depth, count, or cycle limit"})
			continue
		}
		seen[download.URL] = true
		*count = *count + 1
		childContent, childType, err := fetch(ctx, download.URL, opts)
		if err != nil {
			node.add(Finding{Line: download.Line, Severity: "medium", RuleID: "nested-fetch", Message: "Nested script could not be fetched: " + err.Error()})
			continue
		}
		child, err := reviewContent(ctx, download.URL, childContent, opts, depth+1, seen, count)
		if err != nil {
			return nil, err
		}
		if strings.Contains(strings.ToLower(childType), "text/html") {
			child.add(Finding{Severity: "high", RuleID: "html-response", Message: "Nested response is HTML"})
		}
		node.Children = append(node.Children, child)
		if rank(child.Risk) > rank(node.Risk) {
			node.Risk = child.Risk
		}
	}
	if opts.Evaluator != "" {
		result, err := reviewAI(ctx, node, opts)
		if err != nil {
			return nil, fmt.Errorf("AI review failed: %w", err)
		}
		node.AI = result
		for _, finding := range result.Findings {
			node.add(finding)
		}
		if rank(result.Risk) > rank(node.Risk) {
			node.Risk = result.Risk
		}
	}
	return node, nil
}
