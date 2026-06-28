package comments

import (
	"encoding/json"
	"fmt"
	"io"
	"text/tabwriter"
)

// StatsRenderer renders a StatsResult in text or JSON form.
type StatsRenderer struct {
	out io.Writer
}

// NewStatsRenderer creates a renderer that writes to the provided stream.
func NewStatsRenderer(out io.Writer) *StatsRenderer {
	return &StatsRenderer{out: out}
}

// Render writes the stats result in the requested format.
func (r *StatsRenderer) Render(result *StatsResult, format string) error {
	if result == nil {
		return fmt.Errorf("stats result is nil")
	}
	switch format {
	case "json":
		return r.writeJSON(result)
	case "text", "":
		return r.writeText(result)
	default:
		return fmt.Errorf("unknown --format %q (allowed: text, json)", format)
	}
}

func (r *StatsRenderer) writeJSON(result *StatsResult) error {
	enc := json.NewEncoder(r.out)
	enc.SetIndent("", "  ")
	return enc.Encode(result)
}

func (r *StatsRenderer) writeText(result *StatsResult) error {
	tw := tabwriter.NewWriter(r.out, 0, 0, 2, ' ', 0)
	if _, err := fmt.Fprintln(tw, "KEY\tCOUNT\tREVIEWERS\tREPOS\tCHANGES_REQUESTED\tEXAMPLE"); err != nil {
		return err
	}
	for _, row := range result.Rows {
		if _, err := fmt.Fprintf(tw, "%s\t%d\t%d\t%d\t%d\t%s\n", row.Key, row.Count, row.Reviewers, row.Repos, row.Blocking, row.ExampleURL); err != nil {
			return err
		}
	}
	return tw.Flush()
}