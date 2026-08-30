package attestation

import (
	"fmt"

	"github.com/srz-zumix/go-gh-extension/pkg/render"
)

// tableHeader is the shared column layout used to render both a single tag
// list (attestation view <file> / <asset-url>) and a list of PR assets
// (attestation view --pr) as a table.
var tableHeader = []string{"FILENAME", "LOCATION", "COMMIT", "BRANCH", "AUTHOR", "COMMENT"}

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

// RenderTagsTable renders a single tag list (local file or asset URL mode) as
// a one-row table using the same column layout as RenderPRAssets, or as
// exported data (e.g. JSON) when r has an exporter configured.
func RenderTagsTable(r *render.Renderer, filename string, tags []Tag) error {
	if r.HasExporter() {
		return r.RenderExportedData(tags)
	}
	tw := r.NewTableWriter(tableHeader)
	tw.Append(tagsTableRow(filename, "", tags))
	return tw.Render()
}

// RenderPRAssets renders assets found while scanning a pull request
// (attestation view --pr) as a table, or as exported data (e.g. JSON) when r
// has an exporter configured. Assets with no embedded attestation are shown
// with empty COMMIT/BRANCH/AUTHOR/COMMENT cells.
func RenderPRAssets(r *render.Renderer, assets []*PRAsset) error {
	if r.HasExporter() {
		return r.RenderExportedData(assets)
	}
	tw := r.NewTableWriter(tableHeader)
	for _, asset := range assets {
		tw.Append(tagsTableRow(asset.Filename, asset.locationLabel(), asset.Tags))
	}
	return tw.Render()
}

// RenderPRAssetsText renders assets found while scanning a pull request
// (attestation view --pr --format text) as one "key=value" block per file,
// separated by blank lines, or as exported data (e.g. JSON) when r has an
// exporter configured. Assets with no embedded attestation or a read error
// are noted instead of listing tags.
func RenderPRAssetsText(r *render.Renderer, assets []*PRAsset) error {
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

// tagsTableRow builds a table row for the shared FILENAME/LOCATION/COMMIT/
// BRANCH/AUTHOR/COMMENT column layout, leaving cells for tags that were not
// found empty.
func tagsTableRow(filename, location string, tags []Tag) []string {
	byKey := make(map[string]string, len(tags))
	for _, tag := range tags {
		byKey[tag.Key] = tag.Value
	}
	return []string{
		filename,
		location,
		shortCommit(byKey[GitTagCommit]),
		byKey[GitTagBranch],
		byKey[GitTagAuthor],
		byKey[CommentTag],
	}
}

// shortCommit truncates a full commit SHA to its short (8-character) form,
// returning it unchanged when shorter.
func shortCommit(sha string) string {
	const shortLen = 8
	if len(sha) <= shortLen {
		return sha
	}
	return sha[:shortLen]
}

// locationLabel formats the asset's location as "body",
// "issue_comment#<id>", or "review_comment#<id>" for table display.
func (a *PRAsset) locationLabel() string {
	if a.LocationID == 0 {
		return string(a.Location)
	}
	return fmt.Sprintf("%s#%d", a.Location, a.LocationID)
}
