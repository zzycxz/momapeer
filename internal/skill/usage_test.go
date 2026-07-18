package skill

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestUsageTrackerNoOpWhenNoStateDir(t *testing.T) {
	u := NewUsageTracker("") // no state dir → no-op
	u.Record("explore")
	if got := u.LastUsed("explore"); !got.IsZero() {
		t.Fatalf("no-op tracker should return zero time, got %v", got)
	}
	if got := u.ColdSkillNames(time.Hour, true, []string{"explore"}); got != nil {
		t.Fatalf("no-op tracker should return nil cold names, got %v", got)
	}
}

func TestUsageTrackerRecordAndLastUsed(t *testing.T) {
	u := NewUsageTracker(t.TempDir())
	before := time.Now().UTC().Add(-time.Second)
	u.Record("explore")
	last := u.LastUsed("explore")
	if last.IsZero() {
		t.Fatal("LastUsed should be non-zero after Record")
	}
	if last.Before(before) {
		t.Fatalf("LastUsed %v should be after %v", last, before)
	}
	// A skill never recorded has a zero LastUsed.
	if got := u.LastUsed("never-called"); !got.IsZero() {
		t.Fatalf("unrecorded skill should have zero LastUsed, got %v", got)
	}
}

func TestUsageTrackerColdSkillNames(t *testing.T) {
	u := NewUsageTracker(t.TempDir())
	u.Record("recent")
	// Simulate an old usage by writing a stale timestamp directly.
	u.mu.Lock()
	st := u.loadLocked()
	if st.Skills == nil {
		st.Skills = map[string]usageEntry{}
	}
	st.Skills["old"] = usageEntry{LastUsed: time.Now().UTC().Add(-100 * 24 * time.Hour).Format(time.RFC3339)}
	u.storeLocked(st)
	u.mu.Unlock()

	known := []string{"recent", "old", "untouched"}
	// 10-day threshold: "old" is cold, "recent" is not, "untouched" depends on flag.
	cold := u.ColdSkillNames(10*24*time.Hour, false, known)
	if !contains(cold, "old") {
		t.Errorf("expected 'old' in cold list, got %v", cold)
	}
	if contains(cold, "recent") {
		t.Errorf("'recent' should not be cold, got %v", cold)
	}
	if contains(cold, "untouched") {
		t.Errorf("'untouched' should not be cold when includeNeverUsed=false, got %v", cold)
	}
	// With includeNeverUsed=true, untouched becomes cold too.
	coldAll := u.ColdSkillNames(10*24*time.Hour, true, known)
	if !contains(coldAll, "untouched") {
		t.Errorf("expected 'untouched' in cold list when includeNeverUsed=true, got %v", coldAll)
	}
}

func TestUsageTrackerSurvivesCorruptFile(t *testing.T) {
	dir := t.TempDir()
	// Write garbage to the state file.
	path := filepath.Join(dir, usageStateName)
	if err := writeFile(path, []byte("not json {{{")); err != nil {
		t.Fatal(err)
	}
	u := NewUsageTracker(dir)
	// A corrupt file must not panic; LastUsed returns zero.
	if got := u.LastUsed("anything"); !got.IsZero() {
		t.Fatalf("corrupt file should yield zero LastUsed, got %v", got)
	}
	// Record should still work (overwrites the garbage).
	u.Record("explore")
	if got := u.LastUsed("explore"); got.IsZero() {
		t.Fatal("Record after corrupt file should work")
	}
}

func TestUsageTrackerTrimEvictsOldest(t *testing.T) {
	u := NewUsageTracker(t.TempDir())
	// Record usageHistory+5 skills; the oldest should be evicted on the next
	// Record that triggers a trim.
	base := time.Now().UTC()
	for i := 0; i < usageHistory+5; i++ {
		u.mu.Lock()
		st := u.loadLocked()
		if st.Skills == nil {
			st.Skills = map[string]usageEntry{}
		}
		name := "skill-" + string(rune('a'+i%26)) + "-" + itoa(i)
		st.Skills[name] = usageEntry{
			LastUsed: base.Add(-time.Duration(usageHistory+5-i) * time.Hour).Format(time.RFC3339),
			Count:    1,
		}
		u.storeLocked(st)
		u.mu.Unlock()
	}
	// Now trigger a trim by recording one more (Record calls trimLocked).
	u.Record("trigger")
	// The total should not exceed usageHistory.
	u.mu.Lock()
	st := u.loadLocked()
	u.mu.Unlock()
	if len(st.Skills) > usageHistory {
		t.Errorf("expected at most %d entries after trim, got %d", usageHistory, len(st.Skills))
	}
}

// contains checks slice membership.
func contains(s []string, v string) bool {
	for _, x := range s {
		if x == v {
			return true
		}
	}
	return false
}

// itoa is a dependency-free int→string to avoid importing strconv in test.
func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var b []byte
	for n > 0 {
		b = append([]byte{byte('0' + n%10)}, b...)
		n /= 10
	}
	return string(b)
}

// writeFile writes data to path with default perms.
func writeFile(path string, data []byte) error {
	return os.WriteFile(path, data, 0o644)
}
