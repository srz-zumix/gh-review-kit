package attestation

import (
	"fmt"

	"github.com/cli/cli/v2/pkg/cmdutil"
	"github.com/spf13/cobra"
	"github.com/srz-zumix/gh-review-kit/pkg/attestation"
	"github.com/srz-zumix/go-gh-extension/pkg/cmdflags"
	"github.com/srz-zumix/go-gh-extension/pkg/render"
)

// readGitMetadata is the attestation view workflow invoked by the command.
// It is a package variable so tests can substitute a fake implementation
// without invoking ffprobe.
var readGitMetadata = attestation.ReadGitMetadata

// NewViewCmd creates a new command to display Git provenance metadata
// embedded in a video, PNG, or JPEG file.
func NewViewCmd() *cobra.Command {
	var (
		format   string
		exporter cmdutil.Exporter
	)

	cmd := &cobra.Command{
		Use:   "view <input-file>",
		Short: "Display Git provenance metadata embedded in a video, PNG, or JPEG file",
		Long: `Display Git provenance metadata embedded in a video, PNG, or JPEG file.

This command reads the metadata tags previously embedded by 'attestation
set', without modifying the file. Video files are probed with ffprobe; PNG
and JPEG files are read natively.

Requires ffprobe to be available on PATH for video files; PNG and JPEG
files have no external tool dependency.`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			input := args[0]
			ctx := cmd.Context()

			result, err := readGitMetadata(ctx, attestation.ReadOptions{Input: input})
			if err != nil {
				return fmt.Errorf("failed to read git metadata from %q: %w", input, err)
			}

			return attestation.Render(render.NewRenderer(exporter), result.Tags)
		},
	}

	if err := cmdflags.AddFormatFlags(cmd, &exporter, &format, "text", []string{"text"}); err != nil {
		panic(err)
	}

	return cmd
}
