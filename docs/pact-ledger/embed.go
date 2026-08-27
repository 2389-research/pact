// ABOUTME: Embeds the canonical PACT agent skill in the compiled Go binary.
// ABOUTME: Keeps quickstart output byte-for-byte identical to the checked-in skill.
package pactledger

import _ "embed"

//go:embed SKILL.md
var skill string

// Skill returns the canonical standalone PACT agent instructions.
func Skill() string {
	return skill
}
