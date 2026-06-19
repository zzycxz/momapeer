package skillstore

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

const manifestFile = ".store-manifest.json"

// Manager handles installing and tracking store skills in a local skills
// directory (typically .momapeer/skills/).
type Manager struct {
	Dir string // absolute path to the skills directory
}

// NewManager creates a manager rooted at dir.
func NewManager(dir string) *Manager {
	return &Manager{Dir: dir}
}

// ManifestPath returns the path to the store manifest file.
func (m *Manager) ManifestPath() string {
	return filepath.Join(m.Dir, manifestFile)
}

// LoadManifest reads the manifest from disk. Returns an empty manifest if the
// file doesn't exist.
func (m *Manager) LoadManifest() (*Manifest, error) {
	data, err := os.ReadFile(m.ManifestPath())
	if os.IsNotExist(err) {
		return &Manifest{Installed: map[string]InstalledSkill{}}, nil
	}
	if err != nil {
		return nil, err
	}
	var mf Manifest
	if err := json.Unmarshal(data, &mf); err != nil {
		return &Manifest{Installed: map[string]InstalledSkill{}}, nil
	}
	if mf.Installed == nil {
		mf.Installed = map[string]InstalledSkill{}
	}
	return &mf, nil
}

// saveManifest writes the manifest to disk.
func (m *Manager) saveManifest(mf *Manifest) error {
	data, err := json.MarshalIndent(mf, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(m.ManifestPath(), data, 0o644)
}

// Install writes downloaded skill files into the skills directory and records
// the installation in the manifest. The files map is relative path → content;
// a SKILL.md entry is required.
func (m *Manager) Install(slug string, files map[string][]byte, source, version string) error {
	if _, ok := files["SKILL.md"]; !ok {
		// Also accept skill.md (case-insensitive on case-insensitive FS).
		found := false
		for name := range files {
			if strings.EqualFold(name, "SKILL.md") {
				found = true
				break
			}
		}
		if !found {
			return fmt.Errorf("skill %q: archive does not contain SKILL.md", slug)
		}
	}

	destDir := filepath.Join(m.Dir, slug)
	if err := os.MkdirAll(destDir, 0o755); err != nil {
		return err
	}

	for relPath, content := range files {
		full := filepath.Join(destDir, filepath.FromSlash(relPath))
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			return err
		}
		if err := os.WriteFile(full, content, 0o644); err != nil {
			return err
		}
	}

	mf, err := m.LoadManifest()
	if err != nil {
		return err
	}
	if mf.Sources == nil {
		mf.Sources = map[string]string{}
	}
	mf.Installed[slug] = InstalledSkill{
		Slug:        slug,
		Name:        slug,
		Version:     version,
		Source:      source,
		InstalledAt: time.Now().UTC(),
		Dir:         destDir,
	}
	return m.saveManifest(mf)
}

// Uninstall removes a skill directory and its manifest entry.
func (m *Manager) Uninstall(slug string) error {
	mf, err := m.LoadManifest()
	if err != nil {
		return err
	}
	inst, ok := mf.Installed[slug]
	if !ok {
		return fmt.Errorf("skill %q is not installed from store", slug)
	}
	if inst.Dir != "" {
		_ = os.RemoveAll(inst.Dir)
	} else {
		_ = os.RemoveAll(filepath.Join(m.Dir, slug))
	}
	delete(mf.Installed, slug)
	return m.saveManifest(mf)
}

// IsInstalled reports whether a slug is installed and its version.
func (m *Manager) IsInstalled(slug string) (bool, string) {
	mf, err := m.LoadManifest()
	if err != nil {
		return false, ""
	}
	inst, ok := mf.Installed[slug]
	if !ok {
		return false, ""
	}
	return true, inst.Version
}

// InstalledSkills returns all installed store skills, sorted by name.
func (m *Manager) InstalledSkills() []InstalledSkill {
	mf, err := m.LoadManifest()
	if err != nil {
		return nil
	}
	out := make([]InstalledSkill, 0, len(mf.Installed))
	for _, inst := range mf.Installed {
		out = append(out, inst)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

// CheckUpdates compares installed versions against the remote latest. Only
// skills whose latest version differs from the installed version are returned.
func (m *Manager) CheckUpdates(ctx context.Context, client *Client) ([]UpdateInfo, error) {
	mf, err := m.LoadManifest()
	if err != nil {
		return nil, err
	}
	if len(mf.Installed) == 0 {
		return nil, nil
	}
	var updates []UpdateInfo
	for slug, inst := range mf.Installed {
		detail, err := client.GetSkillDetail(ctx, slug)
		if err != nil || detail == nil || detail.LatestVersion == nil {
			continue
		}
		latest := detail.LatestVersion.Version
		if latest != "" && latest != inst.Version {
			updates = append(updates, UpdateInfo{
				Slug:             slug,
				Name:             firstNonEmpty(detail.Skill.DisplayName, slug),
				InstalledVersion: inst.Version,
				LatestVersion:    latest,
				Changelog:        detail.LatestVersion.Changelog,
			})
		}
	}
	sort.Slice(updates, func(i, j int) bool { return updates[i].Name < updates[j].Name })
	return updates, nil
}

func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if strings.TrimSpace(v) != "" {
			return v
		}
	}
	return ""
}
