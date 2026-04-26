/*
Copyright © 2025 srz_zumix
*/
package main

import (
	"embed"

	"github.com/srz-zumix/gh-review-kit/cmd"
)

//go:embed skills
var skillsFS embed.FS

func main() {
	cmd.RegisterSkillsCmd(skillsFS)
	cmd.Execute()
}
