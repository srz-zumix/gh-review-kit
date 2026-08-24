/*
Copyright © 2025 NAME HERE <EMAIL ADDRESS>
*/
package cmd

import (
	"github.com/spf13/cobra"
	"github.com/srz-zumix/gh-review-kit/cmd/attestation"
)

// NewAttestationCmd creates a new parent command for embedding and viewing
// Git provenance metadata in video files.
func NewAttestationCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "attestation",
		Short: "Manage Git provenance metadata embedded in video files",
		Long:  `Manage Git provenance metadata embedded in video files.`,
	}

	cmd.AddCommand(attestation.NewSetCmd())
	cmd.AddCommand(attestation.NewViewCmd())

	return cmd
}

func init() {
	rootCmd.AddCommand(NewAttestationCmd())
}
