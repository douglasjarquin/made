// Package skill is the single source of truth for the content of the /made
// agent skill. Its Markdown constant/body is rendered to the committed
// skills/made/SKILL.md by cmd/genskill; that file must never be hand-edited,
// since any direct edit is silently overwritten (and flagged as drift) the
// next time the generator runs.
package skill
