package agent

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

const (
	// DreamIntervalDays is how often the dream agent runs automatically.
	DreamIntervalDays = 7
	// DistillIntervalDays is how often the distill agent runs automatically.
	DistillIntervalDays = 30
	// minSpawnGap prevents rapid-fire dream/distill triggers.
	minSpawnGap = 10 * time.Second
)

var lastDreamSpawn, lastDistillSpawn time.Time

// DreamTask is the prompt fed to a background agent for memory consolidation.
const DreamTask = `You are a memory consolidation agent. Your job is to review recent session history and extract durable knowledge into project memory.

## Instructions

1. Read the current MEMORY.md and any existing memory files to understand what's already saved.
2. Review recent sessions for:
   - Architecture decisions and their rationale
   - Patterns discovered (coding conventions, project structure, gotchas)
   - User preferences and feedback
   - Solutions to problems that took significant effort
3. For each piece of durable knowledge:
   - Use the remember tool to save it with type "project" or "reference"
   - Include WHY it matters and HOW to apply it
   - Avoid duplicating existing memories
4. If you find memories that are now outdated or contradicted by recent sessions, use the forget tool to archive them.
5. Do NOT save transient information (specific file contents, temporary debugging notes).
6. Focus on knowledge that would help a future session be more effective.

## What to save
- Project architecture and key file locations
- Build/test/lint commands and their quirks
- Coding conventions specific to this project
- Known issues and their workarounds
- User's communication preferences and expertise level
- External service integrations and their configurations

## What NOT to save
- File contents that can be re-read
- Temporary debugging state
- Information already in the codebase
- Generic programming knowledge`

// DistillTask is the prompt fed to a background agent for workflow extraction.
const DistillTask = `You are a workflow distillation agent. Your job is to review recent sessions and identify repeated manual workflows that could be automated.

## Instructions

1. Review recent session history for patterns where the same sequence of steps was repeated across multiple sessions.
2. For each repeated workflow:
   - Create a skill file (.md) that documents the workflow
   - Include clear step-by-step instructions
   - Reference specific tools and commands needed
   - Make it reusable across similar tasks
3. Save skills to .momapeer/skills/ directory.
4. Focus on workflows that would save significant time if automated.

## Good candidates for skills
- Common debugging sequences (e.g., "investigate test failure" → check logs → reproduce → fix → verify)
- Project setup patterns (e.g., "add new feature" → create branch → implement → test → PR)
- Repetitive code patterns (e.g., "add new API endpoint" → handler → route → test → docs)
- Multi-step build/deploy processes

## Not good candidates
- One-off tasks unlikely to repeat
- Tasks too specific to a single bug/feature
- Simple single-tool operations`

// ShouldAutoDream reports whether the dream agent should run based on the last
// dream session timestamp and the project's age.
func ShouldAutoDream(sessionDir string) bool {
	return shouldAutoRun(sessionDir, "Auto Dream", DreamIntervalDays, &lastDreamSpawn)
}

// ShouldAutoDistill reports whether the distill agent should run.
func ShouldAutoDistill(sessionDir string) bool {
	return shouldAutoRun(sessionDir, "Auto Distill", DistillIntervalDays, &lastDistillSpawn)
}

func shouldAutoRun(sessionDir, title string, intervalDays int, lastSpawn *time.Time) bool {
	if sessionDir == "" {
		return false
	}
	// Rate limit.
	if time.Since(*lastSpawn) < minSpawnGap {
		return false
	}
	// Check last session with matching title.
	interval := time.Duration(intervalDays) * 24 * time.Hour
	lastRun := findLastSessionTime(sessionDir, title)
	if lastRun.IsZero() {
		// Never ran. Check if project is old enough.
		firstSession := findFirstSessionTime(sessionDir)
		if firstSession.IsZero() || time.Since(firstSession) < interval {
			return false
		}
	}
	if !lastRun.IsZero() && time.Since(lastRun) < interval {
		return false
	}
	*lastSpawn = time.Now()
	return true
}

// findLastSessionTime scans session files for the most recent one whose topic
// title matches. Returns zero time if none found.
func findLastSessionTime(sessionDir, title string) time.Time {
	sessionsDir := filepath.Join(filepath.Dir(sessionDir), "sessions")
	entries, err := os.ReadDir(sessionsDir)
	if err != nil {
		return time.Time{}
	}
	var latest time.Time
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".jsonl.meta") {
			continue
		}
		// Quick check: read the meta file for topic title.
		metaPath := filepath.Join(sessionsDir, e.Name())
		b, err := os.ReadFile(metaPath)
		if err != nil {
			continue
		}
		if strings.Contains(string(b), fmt.Sprintf(`"topicTitle":"%s"`, title)) {
			info, err := e.Info()
			if err == nil && info.ModTime().After(latest) {
				latest = info.ModTime()
			}
		}
	}
	return latest
}

// findFirstSessionTime returns the earliest session creation time.
func findFirstSessionTime(sessionDir string) time.Time {
	sessionsDir := filepath.Join(filepath.Dir(sessionDir), "sessions")
	entries, err := os.ReadDir(sessionsDir)
	if err != nil {
		return time.Time{}
	}
	var earliest time.Time
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".jsonl.meta") {
			continue
		}
		info, err := e.Info()
		if err == nil {
			if earliest.IsZero() || info.ModTime().Before(earliest) {
				earliest = info.ModTime()
			}
		}
	}
	return earliest
}
