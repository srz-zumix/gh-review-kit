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

// RenderPRAssets renders assets found while scanning a pull request
// (attestation view --pr) as one "key=value" block per file, separated by
// blank lines, or as exported data (e.g. JSON) when r has an exporter
// configured. Assets with no embedded attestation or a read error are noted
// instead of listing tags.
func RenderPRAssets(r *render.Renderer, assets []*PRAsset) error {
	if r.HasExporter() {
		return r.RenderExportedData(assets)
	}
	for i, asset := range assets {
		if i > 0 {
			r.WriteLine("")
		}
		r.WriteLine(fmt.Sprintf("%s (%s)", asset.Filename, asset.locationLabel()))
		switch {
		case asset.Error != "":
			r.WriteLine(fmt.Sprintf("error=%s", asset.Error))
		case !asset.Attested:
			r.WriteLine("no attestation found")
		default:
			for _, tag := range asset.Tags {
				r.WriteLine(fmt.Sprintf("%s=%s", tag.Key, tag.Value))
			}
		}
	}
	return nil
}

// locationLabel formats the asset's location as "body",
// "issue_comment#<id>", or "review_comment#<id>" for text display.
func (a *PRAsset) locationLabel() string {
	if a.LocationID == 0 {
		return string(a.Location)
	}
	return fmt.Sprintf("%s#%d", a.Location, a.LocationID)
}
