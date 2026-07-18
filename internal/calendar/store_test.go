package calendar

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestStoreCRUD(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "test.db")
	s, err := Open(dbPath)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer s.Close()

	// Create
	now := time.Now().Truncate(time.Minute)
	e := &Event{
		Title:     "Test Event",
		StartTime: now,
		EndTime:   now.Add(time.Hour),
		Location:  "Room A",
		Reminders: []int{15},
		Tags:      []string{"work"},
	}
	if err := s.Create(e); err != nil {
		t.Fatalf("create: %v", err)
	}
	if e.ID == "" {
		t.Fatal("expected ID to be set")
	}

	// Get
	got, err := s.Get(e.ID)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if got.Title != "Test Event" {
		t.Errorf("title = %q, want %q", got.Title, "Test Event")
	}
	if got.Location != "Room A" {
		t.Errorf("location = %q, want %q", got.Location, "Room A")
	}
	if len(got.Reminders) != 1 || got.Reminders[0] != 15 {
		t.Errorf("reminders = %v, want [15]", got.Reminders)
	}

	// Update
	got.Title = "Updated Event"
	if err := s.Update(got); err != nil {
		t.Fatalf("update: %v", err)
	}
	got2, _ := s.Get(e.ID)
	if got2.Title != "Updated Event" {
		t.Errorf("updated title = %q, want %q", got2.Title, "Updated Event")
	}

	// List
	events, err := s.List(now.Add(-time.Hour), now.Add(2*time.Hour))
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(events) != 1 {
		t.Errorf("list returned %d events, want 1", len(events))
	}

	// Search
	results, err := s.Search("Updated", 10)
	if err != nil {
		t.Fatalf("search: %v", err)
	}
	if len(results) != 1 {
		t.Errorf("search returned %d results, want 1", len(results))
	}

	// Delete
	if err := s.Delete(e.ID); err != nil {
		t.Fatalf("delete: %v", err)
	}
	_, err = s.Get(e.ID)
	if err == nil {
		t.Error("expected error after delete, got nil")
	}
}

func TestStoreAllDay(t *testing.T) {
	dir := t.TempDir()
	s, err := Open(filepath.Join(dir, "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()

	e := &Event{
		Title:     "All Day Event",
		StartTime: time.Date(2026, 7, 7, 0, 0, 0, 0, time.Local),
		EndTime:   time.Date(2026, 7, 8, 0, 0, 0, 0, time.Local),
		AllDay:    true,
	}
	if err := s.Create(e); err != nil {
		t.Fatal(err)
	}
	got, _ := s.Get(e.ID)
	if !got.AllDay {
		t.Error("expected all_day=true")
	}
}

func TestStoreRecurrence(t *testing.T) {
	dir := t.TempDir()
	s, err := Open(filepath.Join(dir, "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()

	e := &Event{
		Title:         "Weekly Meeting",
		StartTime:     time.Now(),
		EndTime:       time.Now().Add(time.Hour),
		Recurrence:    "FREQ=WEEKLY;BYDAY=MO",
		RecurrenceEnd: time.Now().AddDate(0, 3, 0),
	}
	if err := s.Create(e); err != nil {
		t.Fatal(err)
	}
	got, _ := s.Get(e.ID)
	if got.Recurrence != "FREQ=WEEKLY;BYDAY=MO" {
		t.Errorf("recurrence = %q", got.Recurrence)
	}
	if got.RecurrenceEnd.IsZero() {
		t.Error("expected recurrence_end to be set")
	}
}

func TestStoreSearch(t *testing.T) {
	dir := t.TempDir()
	s, err := Open(filepath.Join(dir, "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()

	s.Create(&Event{Title: "周会", StartTime: time.Now(), EndTime: time.Now().Add(time.Hour)})
	s.Create(&Event{Title: "代码review", StartTime: time.Now(), EndTime: time.Now().Add(time.Hour)})
	s.Create(&Event{Title: "周报", StartTime: time.Now(), EndTime: time.Now().Add(time.Hour)})

	results, _ := s.Search("周", 10)
	if len(results) != 2 {
		t.Errorf("search '周' returned %d, want 2", len(results))
	}
}

func TestStoreExceptions(t *testing.T) {
	dir := t.TempDir()
	s, err := Open(filepath.Join(dir, "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()

	e := &Event{
		Title:      "Weekly",
		StartTime:  time.Now(),
		EndTime:    time.Now().Add(time.Hour),
		Recurrence: "FREQ=WEEKLY",
	}
	s.Create(e)

	ex := &Exception{
		EventID:      e.ID,
		OriginalDate: time.Date(2026, 7, 14, 0, 0, 0, 0, time.Local),
		NewStart:     time.Date(2026, 7, 15, 10, 0, 0, 0, time.Local),
		NewEnd:       time.Date(2026, 7, 15, 11, 0, 0, 0, time.Local),
	}
	if err := s.AddException(ex); err != nil {
		t.Fatal(err)
	}

	excs, _ := s.GetExceptions(e.ID)
	if len(excs) != 1 {
		t.Fatalf("expected 1 exception, got %d", len(excs))
	}
	if excs[0].ID == "" {
		t.Error("expected exception ID to be set")
	}
}

func TestMain(m *testing.M) {
	os.Exit(m.Run())
}
