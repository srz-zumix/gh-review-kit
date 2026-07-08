package comments

import (
	"fmt"
	"strconv"

	"github.com/srz-zumix/go-gh-extension/pkg/render"
)

// RenderStats writes the stats result using the provided renderer.
// When an exporter is configured (e.g. --format json) the structured result is
// emitted; otherwise a table is rendered.
func RenderStats(r *render.Renderer, result *StatsResult) error {
	if result == nil {
		return fmt.Errorf("stats result is nil")
	}
	if r.HasExporter() {
		return r.RenderExportedData(result)
	}
	tw := r.NewTableWriter([]string{"KEY", "COUNT", "REVIEWERS", "REPOS", "CHANGES_REQUESTED", "EXAMPLE"})
	for _, row := range result.Rows {
		tw.Append([]string{
			row.Key,
			strconv.Itoa(row.Count),
			strconv.Itoa(row.Reviewers),
			strconv.Itoa(row.Repos),
			strconv.Itoa(row.Blocking),
			row.ExampleURL,
		})
	}
	return tw.Render()
}
