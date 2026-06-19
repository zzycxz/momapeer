package skillstore

import "time"

// SkillSummary is a ClawHub skill list/search result item.
type SkillSummary struct {
	Slug          string            `json:"slug"`
	DisplayName   string            `json:"displayName"`
	Summary       string            `json:"summary"`
	Tags          map[string]string `json:"tags"`
	Stats         SkillStats        `json:"stats"`
	CreatedAt     int64             `json:"createdAt"`
	UpdatedAt     int64             `json:"updatedAt"`
	LatestVersion *VersionInfo      `json:"latestVersion"`
	// Search-only fields
	Score float64 `json:"score,omitempty"`
	Owner *Owner  `json:"owner,omitempty"`
}

// SkillStats holds engagement metrics.
type SkillStats struct {
	Stars           int `json:"stars"`
	InstallsCurrent int `json:"installsCurrent"`
	InstallsAllTime int `json:"installsAllTime"`
	Downloads       int `json:"downloads"`
}

// VersionInfo is a version summary.
type VersionInfo struct {
	Version   string `json:"version"`
	CreatedAt int64  `json:"createdAt"`
	Changelog string `json:"changelog"`
}

// Owner is a publisher profile.
type Owner struct {
	Handle      string `json:"handle"`
	DisplayName string `json:"displayName"`
	Image       string `json:"image"`
}

// SkillDetail is the full detail response.
type SkillDetail struct {
	Skill         SkillSummary  `json:"skill"`
	LatestVersion *VersionInfo  `json:"latestVersion"`
	Owner         *Owner        `json:"owner"`
	Moderation    *Moderation   `json:"moderation"`
}

// Moderation holds safety scan results.
type Moderation struct {
	IsSuspicious    bool   `json:"isSuspicious"`
	IsMalwareBlocked bool  `json:"isMalwareBlocked"`
	Verdict         string `json:"verdict"`
	Summary         string `json:"summary"`
}

// ListOptions controls list fetching.
type ListOptions struct {
	Limit             int
	Cursor            string
	Sort              string // recommended | stars | installs | updated | newest | name
	NonSuspiciousOnly bool
}

// SearchOptions controls search fetching.
type SearchOptions struct {
	Limit             int
	NonSuspiciousOnly bool
}

// ListResult is a paginated skill list.
type ListResult struct {
	Items      []SkillSummary `json:"items"`
	NextCursor string         `json:"nextCursor"`
}

// SearchResult is a search response.
type SearchResult struct {
	Results []SkillSummary `json:"results"`
}

// InstalledSkill records a locally installed store skill.
type InstalledSkill struct {
	Slug        string    `json:"slug"`
	Name        string    `json:"name"`
	Version     string    `json:"version"`
	Source      string    `json:"source"`
	InstalledAt time.Time `json:"installedAt"`
	Dir         string    `json:"dir"` // absolute path to the skill directory
}

// UpdateInfo compares installed vs latest.
type UpdateInfo struct {
	Slug            string `json:"slug"`
	Name            string `json:"name"`
	InstalledVersion string `json:"installedVersion"`
	LatestVersion   string `json:"latestVersion"`
	Changelog       string `json:"changelog"`
}

// Manifest is the .store-manifest.json structure.
type Manifest struct {
	Sources   map[string]string    `json:"sources,omitempty"`
	Installed map[string]InstalledSkill `json:"installed,omitempty"`
}
