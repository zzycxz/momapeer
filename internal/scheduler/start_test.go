package scheduler

import (
	"context"
	"testing"
	"time"
)

// TestStartActuallyLaunchesTimer confirms Start arms the precise timer (not the
// old broken stopCh logic) and a due task fires. With AfterFunc the past-due
// task should fire within ~1s, not the old 30s tick — so this also serves as a
// regression guard for both the Start() bug and the polling→timer migration.
func TestStartActuallyLaunchesTimer(t *testing.T) {
	s := New(t.TempDir() + "/start.json")
	now := time.Now()
	_, err := s.Create(ScheduledTask{
		Name:       "start-test",
		Expression: "at " + now.Add(time.Minute).Format("2006-01-02 15:04"),
		Prompt:     "x",
		OutputMode: "notify",
	})
	if err != nil {
		t.Fatal(err)
	}
	// Backdate NextRun so it's immediately due.
	s.mu.Lock()
	s.tasks[0].NextRun = now.Add(-1 * time.Second)
	s.mu.Unlock()

	notifyCount := 0
	s.SetNotifier(testNotifierCount{&notifyCount})
	s.SetRunner(fakeRunner{fn: func(context.Context, string, string) (string, error) {
		return "ran", nil
	}})
	s.Start()
	defer s.Stop()

	// With AfterFunc, a past-due task fires within ~1s. Allow generous 10s for CI.
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		if notifyCount > 0 {
			return // fired promptly — timer works
		}
		time.Sleep(200 * time.Millisecond)
	}
	t.Fatalf("Start() did not fire a past-due task within 10s (notifyCount=%d)", notifyCount)
}

// TestPreciseFireTiming confirms the AfterFunc timer fires at the scheduled
// second (within a small tolerance), not up to 30s late like the old poller.
// We schedule a task ~3s out and assert it fires within 3s + tolerance.
func TestPreciseFireTiming(t *testing.T) {
	s := New(t.TempDir() + "/precise.json")
	s.SetLogger(func(f string, a ...any) { t.Logf("[LOG] "+f, a...) })
	// Schedule 3 seconds out. Use a 2-minute-future base then backdate NextRun to
	// now+3s (Create needs a future expression to succeed).
	base := time.Now().Add(2 * time.Minute).Format("2006-01-02 15:04")
	_, err := s.Create(ScheduledTask{
		Name:       "precise-test",
		Expression: "at " + base,
		Prompt:     "x",
		OutputMode: "notify",
	})
	if err != nil {
		t.Fatal(err)
	}
	// Arm precisely at now+3s.
	target := time.Now().Add(3 * time.Second)
	s.mu.Lock()
	s.tasks[0].NextRun = target
	s.mu.Unlock()

	var fireTime time.Time
	s.SetNotifier(notifierCaptureTime{&fireTime})
	s.SetRunner(fakeRunner{fn: func(context.Context, string, string) (string, error) {
		return "ran", nil
	}})
	s.Start()
	defer s.Stop()

	// Wait up to 8s. The fire should land within 3s + 2s tolerance.
	deadline := time.Now().Add(8 * time.Second)
	for time.Now().Before(deadline) {
		if !fireTime.IsZero() {
			delta := fireTime.Sub(target)
			t.Logf("fired at %v, target %v, delta %v", fireTime.Format("15:04:05.000"), target.Format("15:04:05.000"), delta)
			// Should be within ±2s of the target — far tighter than the old 30s poll.
			if delta > 2*time.Second {
				t.Errorf("fire delta %v exceeds 2s tolerance (old poller-level imprecision)", delta)
			}
			return
		}
		time.Sleep(100 * time.Millisecond)
	}
	t.Fatal("task did not fire within 8s")
}

type testNotifierCount struct{ count *int }

func (n testNotifierCount) Notify(name, body string) { *n.count++ }

type notifierCaptureTime struct{ t *time.Time }

func (n notifierCaptureTime) Notify(name, body string) { *n.t = time.Now() }
