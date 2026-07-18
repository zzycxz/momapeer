package ppttemplate

import (
	"os"
	"path/filepath"
	"testing"
)

// TestLoadFile parses a well-formed template and checks id/name defaults.
func TestLoadFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "mine.json")
	// id omitted on purpose: should default to filename stem "mine".
	os.WriteFile(path, []byte(`{
		"name": "我的模板",
		"master_file": "C:/x.pptx",
		"theme": { "primary_color": "1A56DB", "font_size_title": 40 },
		"layouts": {
			"content": { "title_x": 8, "title_y": 8, "body_x": 8, "body_y": 25 }
		},
		"default_layout": "content"
	}`), 0o644)

	tpl, err := loadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if tpl.ID != "mine" { // defaulted from filename
		t.Errorf("ID = %q, want mine", tpl.ID)
	}
	if tpl.Name != "我的模板" {
		t.Errorf("Name = %q", tpl.Name)
	}
	if tpl.MasterFile != "C:/x.pptx" {
		t.Errorf("MasterFile = %q", tpl.MasterFile)
	}
	// Layout flat-field unmarshal.
	if l, ok := tpl.Layouts["content"]; !ok {
		t.Fatal("missing content layout")
	} else if l.TitleX != 8 || l.BodyY != 25 {
		t.Errorf("layout coords wrong: %+v", l)
	}
}

// TestLoadDirSkipsMalformed verifies one bad file doesn't break the whole list.
func TestLoadDirSkipsMalformed(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "good.json"), []byte(`{"name":"good"}`), 0o644)
	os.WriteFile(filepath.Join(dir, "bad.json"), []byte(`{not json`), 0o644)
	os.WriteFile(filepath.Join(dir, "ignore.txt"), []byte("x"), 0o644) // non-json skipped

	ts, err := LoadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(ts) != 1 {
		t.Fatalf("want 1 valid template, got %d", len(ts))
	}
	if ts[0].Name != "good" {
		t.Errorf("got %q", ts[0].Name)
	}
}

// TestLoadActive covers: empty id → nil (no error), valid id → found, bad id → error.
func TestLoadActive(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "a.json"), []byte(`{"name":"A"}`), 0o644)

	if tpl, err := LoadActive(dir, ""); err != nil || tpl != nil {
		t.Errorf("empty id: tpl=%v err=%v, want nil,nil", tpl, err)
	}
	if tpl, err := LoadActive(dir, "a"); err != nil || tpl == nil || tpl.Name != "A" {
		t.Errorf("active a: tpl=%v err=%v", tpl, err)
	}
	if _, err := LoadActive(dir, "nope"); err == nil {
		t.Error("bad id should error")
	}
}

// TestDefaultDirSeedsExample verifies DefaultDir creates the dir + example.json.
func TestDefaultDirSeedsExample(t *testing.T) {
	// Use a private temp HOME/AppData so we don't touch the real user dir.
	dir := t.TempDir()
	t.Setenv("AppData", filepath.Join(dir, "AppData")) // Windows UserConfigDir reads AppData
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(dir, "config"))

	ptdir := DefaultDir()
	if ptdir == "" {
		t.Fatal("DefaultDir returned empty")
	}
	// example.json should exist.
	if _, err := os.Stat(filepath.Join(ptdir, "example.json")); err != nil {
		t.Fatalf("example.json not seeded: %v", err)
	}
	// Views should list at least the example.
	vs := Views(ptdir)
	if len(vs) == 0 {
		t.Fatal("no views returned")
	}
}

// TestLayoutRoundTrip checks the custom marshal/unmarshal is symmetric.
func TestLayoutRoundTrip(t *testing.T) {
	orig := Layout{TitleX: 10, TitleY: 20, TitleW: 80, BodyY: 25, BodyH: 65}
	b, err := orig.MarshalJSON()
	if err != nil {
		t.Fatal(err)
	}
	var back Layout
	if err := back.UnmarshalJSON(b); err != nil {
		t.Fatal(err)
	}
	if back != orig {
		t.Errorf("round-trip mismatch: got %+v want %+v", back, orig)
	}
}
