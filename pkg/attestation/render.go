package attestation

import (
	"fmt"

	"github.com/srz-zumix/go-gh-extension/pkg/render"
)

// Render writes tags using r, one "key=value" line per tag, or as exported
// data (e.g. JSON) when r has an exporter configured via --format/--jq.
func Render(r *render.Renderer, tags []Tag) error {
	if r.HasExporter() {
		return r.RenderExportedData(tags)
	}
	for _, tag := range tags {
		r.WriteLine(fmt.Sprintf("%s=%s", tag.Key, tag.Value))
	}
	return nil
}
