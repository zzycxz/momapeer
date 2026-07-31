// Package assets embeds the built-in, cross-platform skill payloads that momapeer
// ships with its binary. Embedding lets a single downloaded executable carry
// everything it needs, instead of distributing a separate .momapeer "tail".
//
// Currently this embeds the ppt-auto skill (SVG → PPTX, pure Python + python-pptx).
// The heavy, platform-specific Python runtime is intentionally NOT embedded: the
// released payload expects the user's system Python 3.10+ and installs its pip
// dependencies on first use (see pptauto/setup_python.*). This keeps the binary
// lean and the skill genuinely cross-platform (macOS/Linux/Windows).
package assets

import "embed"

// pptauto is the embedded ppt-auto skill tree. It is released to the user's
// ~/.momapeer/skills/ppt-auto/ on first run by EnsurePPTAutoSkill.
//
// The `all:` prefix is required: the tree contains entries whose names begin
// with `_` (Python __init__.py, templates/_index.md) which a bare //go:embed
// pattern silently skips, truncating the skill. all: includes them.
//
//go:embed all:pptauto
var pptauto embed.FS
