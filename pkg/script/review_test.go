package script

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestAnalyze(t *testing.T) {
	for _, test := range []struct {
		source string
		want   Risk
	}{
		{"echo hello\n", "none"},
		{"# sudo rm -rf /\necho 'sudo rm -rf /'", "none"},
		{"sudo true", "high"},
		{"rm -rf /", "critical"},
		{"rm -fR /", "critical"},
		{"dd if=/dev/zero of=/dev/sda", "critical"},
		{"env bash -c 'echo hi'", "medium"},
		{"rm -rf \"$HOME\"", "critical"},
		{"eval \"$payload\"", "high"},
		{"echo data >> ~/.bashrc", "high"},
		{"bash -c 'sudo true'", "high"},
		{"echo $(sudo true)", "high"},
		{"echo '", "high"},
	} {
		t.Run(test.source, func(t *testing.T) {
			findings, _ := Analyze([]byte(test.source))
			risk := Risk("none")
			for _, finding := range findings {
				if rank(finding.Severity) > rank(risk) {
					risk = finding.Severity
				}
			}
			if risk != test.want {
				t.Fatalf("risk = %s, want %s: %+v", risk, test.want, findings)
			}
		})
	}
}

func TestReviewFetch(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.URL.Path == "/parent" {
			_, _ = writer.Write([]byte("curl 'http://" + request.Host + "/child' | bash"))
			return
		}
		_, _ = writer.Write([]byte("sudo true"))
	}))
	defer server.Close()
	opts := DefaultOptions()
	if _, err := Review(context.Background(), server.URL+"/parent", nil, opts); err == nil {
		t.Fatal("HTTP accepted")
	}
	opts.AllowHTTP = true
	node, err := Review(context.Background(), server.URL+"/parent", nil, opts)
	if err != nil {
		t.Fatal(err)
	}
	if node.Risk != "high" || len(node.Children) != 1 {
		t.Fatalf("unexpected review: %+v", node)
	}
	opts.Depth = 0
	node, err = Review(context.Background(), server.URL+"/parent", nil, opts)
	if err != nil || len(node.Children) != 0 || node.Risk != "medium" {
		t.Fatalf("depth limit: %+v %v", node, err)
	}
	opts.MaxSize = 2
	if _, err := Review(context.Background(), server.URL+"/child", nil, opts); err == nil {
		t.Fatal("oversize accepted")
	}
}

func TestAnalyzeSystemConfigurationAccess(t *testing.T) {
	for _, test := range []struct {
		source string
		want   Risk
	}{
		{`cat /etc/example-release`, "low"},
		{`echo "Read /etc/example-release"`, "low"},
		{`grep NAME /etc/example-release`, "low"},
		{`read value < /etc/example-release`, "low"},
		{`cp /etc/example-release /tmp/example-backup`, "low"},
		{`echo data > /etc/example.conf`, "high"},
		{`echo data >> /etc/example.conf`, "high"},
		{`cat <> /etc/example.conf`, "high"},
		{`tee /etc/example.conf`, "high"},
		{`cp ./example.conf /etc/example.conf`, "high"},
		{`cp "$source" /etc/example.conf`, "high"},
		{`cp "$source" -t /etc/example.d`, "high"},
		{`cp -t /etc/example.d ./example.conf`, "high"},
		{`sed -i 's/old/new/' /etc/example.conf`, "high"},
		{`custom_command /etc/example.conf`, "medium"},
		{`cat /etc/example.conf > /etc/example-copy`, "high"},
	} {
		t.Run(test.source, func(t *testing.T) {
			findings, _ := Analyze([]byte(test.source))
			risk := Risk("none")
			for _, finding := range findings {
				if rank(finding.Severity) > rank(risk) {
					risk = finding.Severity
				}
			}
			if risk != test.want {
				t.Fatalf("risk=%s want=%s findings=%+v", risk, test.want, findings)
			}
		})
	}
}

func TestAnalyzeCommandProbe(t *testing.T) {
	for _, test := range []struct {
		source  string
		wrapper bool
	}{
		{`command -v example-tool`, false},
		{`command -v "$tool"`, false},
		{`command -p -V example-tool`, false},
		{`command -pv example-tool`, false},
		{`command -v example-tool; command example-tool`, true},
		{`command -p example-tool`, true},
		{`command "$options" example-tool`, true},
	} {
		findings, _ := Analyze([]byte(test.source))
		found := false
		for _, finding := range findings {
			found = found || finding.RuleID == "command-wrapper"
		}
		if found != test.wrapper {
			t.Fatalf("%s: %+v", test.source, findings)
		}
	}
}

func TestChecksum(t *testing.T) {
	opts := DefaultOptions()
	opts.SHA256 = strings.Repeat("0", 64)
	if _, err := Review(context.Background(), "-", strings.NewReader("echo hi"), opts); err == nil {
		t.Fatal("checksum mismatch accepted")
	}
	opts.SHA256 = digest([]byte("echo hi"))
	if _, err := Review(context.Background(), "-", strings.NewReader("echo hi"), opts); err != nil {
		t.Fatal(err)
	}
}

func TestNestedDownloadPatterns(t *testing.T) {
	for _, source := range []string{
		`curl -o - https://example.com/install | bash`,
		`curl -fsSL https://example.com/install | sh`,
		`wget -O - https://example.com/install | sh`,
		`curl -o ./install https://example.com/install; ./install`,
		`sh -c 'curl https://example.com/install | bash'`,
		`sh -c "$(curl https://example.com/install)"`,
	} {
		_, downloads := Analyze([]byte(source))
		if len(downloads) != 1 || !downloads[0].Script {
			t.Fatalf("%s: %+v", source, downloads)
		}
	}
	findings, _ := Analyze([]byte(`curl "https://example.com/$VERSION" | sh`))
	found := false
	for _, finding := range findings {
		found = found || finding.RuleID == "unresolved-url"
	}
	if !found {
		t.Fatal("dynamic URL not reported")
	}
}

func TestAIValidation(t *testing.T) {
	for _, output := range []string{`{}`, `{"risk":"safe","summary":"fine"}`, `{"risk":"none","summary":"fine","findings":[{"severity":"unknown"}]}`} {
		if _, err := parseAI(output); err == nil {
			t.Fatalf("accepted %s", output)
		}
	}
	node := &Node{Risk: "none"}
	node.add(Finding{Severity: "high"})
	node.add(Finding{Severity: "none"})
	if node.Risk != "high" {
		t.Fatal("risk was lowered")
	}
	result, err := parseAI("progress\n```json\n" + validAI + "\n```\nAI Credits 1")
	if err != nil || result.Risk != "low" {
		t.Fatalf("transcript: %+v %v", result, err)
	}
	if _, err := parseAI(`{"risk":"low","summary":"one"} {"risk":"high","summary":"two"}`); err == nil {
		t.Fatal("ambiguous AI result accepted")
	}
}

const validAI = `{"risk":"low","summary":"reviewed","findings":[],"network_destinations":[],"modified_paths":[],"requires_root":false}`

func TestAIValidationRiskFields(t *testing.T) {
	for _, test := range []struct {
		name   string
		output string
		field  string
	}{
		{
			name:   "overall risk",
			output: strings.Replace(validAI, `"risk":"low"`, `"risk":"info"`, 1),
			field:  "AI risk",
		},
		{
			name:   "finding severity",
			output: strings.Replace(validAI, `"findings":[]`, `"findings":[{"line":1,"severity":"info","rule_id":"notice","message":"Informational finding"}]`, 1),
			field:  "AI findings[0].severity",
		},
		{
			name:   "second finding severity",
			output: strings.Replace(validAI, `"findings":[]`, `"findings":[{"line":1,"severity":"low","rule_id":"notice","message":"First finding"},{"line":2,"severity":"info","rule_id":"notice","message":"Second finding"}]`, 1),
			field:  "AI findings[1].severity",
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			_, err := parseAI(test.output)
			if err == nil || !strings.Contains(err.Error(), test.field) || !strings.Contains(err.Error(), `"info"`) || !strings.Contains(err.Error(), "expected none/low/medium/high/critical") {
				t.Fatalf("expected field-specific error with allowed values, got %v", err)
			}
		})
	}
	for _, risk := range Risks {
		t.Run(string(risk), func(t *testing.T) {
			output := strings.Replace(validAI, `"risk":"low"`, `"risk":"`+string(risk)+`"`, 1)
			output = strings.Replace(output, `"findings":[]`, `"findings":[{"line":1,"severity":"`+string(risk)+`","rule_id":"notice","message":"Review finding"}]`, 1)
			if _, err := parseAI(output); err != nil {
				t.Fatalf("valid risk and severity rejected: %v", err)
			}
		})
	}
}

func TestAIReviewCannotLowerRisk(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell fixture")
	}
	directory := t.TempDir()
	bin := filepath.Join(directory, "ai")
	fixture := "#!/bin/sh\ncase \"$*\" in *--available-tools=view,grep,glob*) ;; *) exit 9 ;; esac\ncase \"$2\" in *'severity (none/low/medium/high/critical)'*) ;; *) exit 10 ;; esac\npwd > \"$REVIEW_CWD\"\nprintf '%s' '" + validAI + "'\n"
	if err := os.WriteFile(bin, []byte(fixture), 0700); err != nil {
		t.Fatal(err)
	}
	log := filepath.Join(directory, "cwd")
	t.Setenv("REVIEW_CWD", log)
	opts := DefaultOptions()
	opts.Evaluator, opts.Bin = "copilot", bin
	node, err := Review(context.Background(), "-", strings.NewReader("sudo true"), opts)
	if err != nil {
		t.Fatal(err)
	}
	if node.Risk != "high" || node.AI == nil {
		t.Fatalf("AI lowered risk: %+v", node)
	}
	cwd, err := os.ReadFile(log)
	if err != nil || !strings.Contains(string(cwd), "gh-review-kit-script-ai-") {
		t.Fatalf("not isolated: %q %v", cwd, err)
	}
	if err := os.WriteFile(bin, []byte("#!/bin/sh\nprintf '%s' '"+validAI+"'\nexit 1\n"), 0700); err != nil {
		t.Fatal(err)
	}
	if _, err := Review(context.Background(), "-", strings.NewReader("echo hi"), opts); err == nil {
		t.Fatal("AI process failure ignored")
	}
}

func TestFetchRedirectsAndChecksums(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		switch request.URL.Path {
		case "/loop":
			http.Redirect(writer, request, "/loop", http.StatusFound)
		case "/invalid":
			http.Redirect(writer, request, "ftp://example.com/install", http.StatusFound)
		case "/missing":
			writer.WriteHeader(http.StatusNotFound)
		case "/sums":
			_, _ = writer.Write([]byte(digest([]byte("echo hi")) + "  install.sh\n"))
		default:
			_, _ = writer.Write([]byte("echo hi"))
		}
	}))
	defer server.Close()
	opts := DefaultOptions()
	opts.AllowHTTP = true
	for _, path := range []string{"/loop", "/invalid", "/missing"} {
		if _, _, err := fetch(context.Background(), server.URL+path, opts); err == nil {
			t.Fatalf("accepted %s", path)
		}
	}
	opts.ChecksumsURL = server.URL + "/sums"
	if _, err := Review(context.Background(), server.URL+"/install.sh", nil, opts); err != nil {
		t.Fatal(err)
	}
	if _, err := Review(context.Background(), server.URL+"/other.sh", nil, opts); err == nil {
		t.Fatal("missing checksum accepted")
	}
}
