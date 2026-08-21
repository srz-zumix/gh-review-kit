/*
Copyright © 2025 NAME HERE <EMAIL ADDRESS>
*/
package cmd

import (
	"os"

	"github.com/spf13/cobra"
	"github.com/srz-zumix/gh-review-kit/version"
	"github.com/srz-zumix/go-gh-extension/pkg/actions"
	"github.com/srz-zumix/go-gh-extension/pkg/cmdflags"
)

var (
	logLevel string
	readOnly bool
)

var rootCmd = &cobra.Command{
	Use:     "gh-review-kit",
	Short:   "A tool to manage GitHub reviews",
	Long:    `gh-review-kit is a tool to manage GitHub reviews.`,
	Version: version.Version,
}

func Execute() {
	err := rootCmd.Execute()
	if err != nil {
		os.Exit(1)
	}
}

func init() {
	if actions.IsRunsOn() {
		rootCmd.SetErrPrefix(actions.GetErrorPrefix())
	}
	cmdflags.AddPersistentFlags(rootCmd)
}
