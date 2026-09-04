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

// RenderAssetUpdates renders the outcome of re-embedding and re-uploading a
// pull request or issue's attachments (attestation set --pr/--issue) as one
// block per asset, separated by blank lines, or as exported data (e.g. JSON)
// when r has an exporter configured.
func RenderAssetUpdates(r *render.Renderer, updates []*AssetUpdate) error {
	if r.HasExporter() {
		return r.RenderExportedData(updates)
	}
	for i, update := range updates {
		if i > 0 {
			r.WriteLine("")
		}
		r.WriteLine(fmt.Sprintf("%s (%s)", update.Filename, update.locationLabel()))
		switch {
		case update.Error != "":
			r.WriteLine(fmt.Sprintf("error=%s", update.Error))
		case update.Skipped != "":
			r.WriteLine(fmt.Sprintf("skipped=%s", update.Skipped))
		default:
			r.WriteLine(fmt.Sprintf("old_url=%s", update.OldURL))
			r.WriteLine(fmt.Sprintf("new_url=%s", update.NewURL))
			if update.Output != "" {
				r.WriteLine(fmt.Sprintf("output=%s", update.Output))
			}
			for _, tag := range update.Tags {
				r.WriteLine(fmt.Sprintf("%s=%s", tag.Key, tag.Value))
			}
		}
	}
	return nil
}

// assetLocationLabel formats an asset location as "body",
// "issue_comment#<id>", or "review_comment#<id>" for text display.
func assetLocationLabel(location AssetLocation, locationID int64) string {
	if locationID == 0 {
		return string(location)
	}
	return fmt.Sprintf("%s#%d", location, locationID)
}

// locationLabel formats the asset's location as "body",
// "issue_comment#<id>", or "review_comment#<id>" for text display.
func (a *PRAsset) locationLabel() string {
	return assetLocationLabel(a.Location, a.LocationID)
}

// locationLabel formats the update's location the same way PRAsset does.
func (a *AssetUpdate) locationLabel() string {
	return assetLocationLabel(a.Location, a.LocationID)
}
