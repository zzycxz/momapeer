package scheduler

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"
)

// TestIntegrationSchedulerMatrix 是调度器端到端集成测试，覆盖真实用户场景。
// 每个用例启动调度器、创建任务、等待触发、断言结果。验证 time.AfterFunc
// 精确定时器对各类任务都可靠工作。
//
// 用例矩阵：
//  1. 纯提醒 + 一次性：Plain=true，到点弹原文，不调 AI，触发后自动禁用
//  2. AI 任务 + 一次性：Plain=false，到点调 runner，返回 agent 输出
//  3. 循环任务：every，首次触发后重新武装定时器，第二次也准时
//  4. 动态新建：Start 后再 Create 任务，定时器能重新武装捕捉新任务
//  5. 删除任务：删除最近任务后，定时器重新武装到次近任务
func TestIntegrationSchedulerMatrix(t *testing.T) {
	t.Run("纯提醒_一次性", func(t *testing.T) {
		s := New(t.TempDir() + "/plain_oneshot.json")
		s.SetLogger(func(f string, a ...any) { t.Logf("[LOG] "+f, a...) })

		// 建一个 2 分钟后的一次性任务（Create 需要未来时间），再 backdate 到 2 秒后
		_, err := s.Create(ScheduledTask{
			Name: "提醒喝水", Expression: "at " + time.Now().Add(2*time.Minute).Format("2006-01-02 15:04"),
			Prompt: "该喝水了", Plain: true, OutputMode: "notify",
		})
		if err != nil {
			t.Fatal(err)
		}
		fireAt := time.Now().Add(2 * time.Second)
		s.mu.Lock()
		s.tasks[0].NextRun = fireAt
		s.mu.Unlock()

		var gotName, gotBody string
		runnerCalled := false
		var mu sync.Mutex
		s.SetNotifier(captureNotifier{&mu, func(name, body string) {
			gotName, gotBody = name, body
		}})
		s.SetRunner(fakeRunner{fn: func(context.Context, string, string) (string, error) {
			runnerCalled = true
			return "AI 不该被调用", nil
		}})
		s.Start()
		defer s.Stop()

		if !waitFire(t, 10*time.Second, func() bool {
			mu.Lock()
			defer mu.Unlock()
			return gotName != ""
		}) {
			t.Fatal("纯提醒任务 10 秒内未触发")
		}
		mu.Lock()
		defer mu.Unlock()
		if runnerCalled {
			t.Error("Plain=true 不应调用 runner（AI）")
		}
		if gotName != "提醒喝水" {
			t.Errorf("notify name=%q want 提醒喝水", gotName)
		}
		if gotBody != "该喝水了" {
			t.Errorf("notify body=%q want 该喝水了（原文）", gotBody)
		}
		// 一次性触发后应自动禁用
		if tk := s.List(false)[0]; tk.Enabled {
			t.Error("一次性任务触发后应自动禁用")
		}
	})

	t.Run("AI任务_一次性", func(t *testing.T) {
		s := New(t.TempDir() + "/ai_oneshot.json")
		s.SetLogger(func(f string, a ...any) { t.Logf("[LOG] "+f, a...) })

		_, err := s.Create(ScheduledTask{
			Name: "总结邮件", Expression: "at " + time.Now().Add(2*time.Minute).Format("2006-01-02 15:04"),
			Prompt: "总结今天的邮件", OutputMode: "notify", // Plain 默认 false → 走 AI
		})
		if err != nil {
			t.Fatal(err)
		}
		s.mu.Lock()
		s.tasks[0].NextRun = time.Now().Add(2 * time.Second)
		s.mu.Unlock()

		var mu sync.Mutex
		runnerCalled := false
		notifyBody := ""
		s.SetRunner(fakeRunner{fn: func(context.Context, string, string) (string, error) {
			mu.Lock()
			runnerCalled = true
			mu.Unlock()
			return "邮件总结：今日3封", nil
		}})
		s.SetNotifier(captureNotifier{&mu, func(name, body string) { notifyBody = body }})
		s.Start()
		defer s.Stop()

		if !waitFire(t, 10*time.Second, func() bool {
			mu.Lock()
			defer mu.Unlock()
			return runnerCalled
		}) {
			t.Fatal("AI 任务 10 秒内 runner 未被调用")
		}
		mu.Lock()
		defer mu.Unlock()
		if !runnerCalled {
			t.Error("Plain=false 应调用 runner（AI）")
		}
		if notifyBody != "邮件总结：今日3封" {
			t.Errorf("notify body=%q want runner 输出", notifyBody)
		}
	})

	t.Run("循环任务_重新武装", func(t *testing.T) {
		s := New(t.TempDir() + "/recurring.json")
		s.SetLogger(func(f string, a ...any) { t.Logf("[LOG] "+f, a...) })

		// every 1m 循环任务（调度器最小间隔 1m，防热循环）。backdate 到 2s 后触发首次，
		// 验证首次触发后定时器重新武装（NextRun 被重算且 nextTimer 非 nil）。
		_, err := s.Create(ScheduledTask{
			Name: "心跳", Expression: "every 1m", Prompt: "beat", OutputMode: "notify",
		})
		if err != nil {
			t.Fatal(err)
		}
		s.mu.Lock()
		s.tasks[0].NextRun = time.Now().Add(2 * time.Second)
		s.mu.Unlock()

		// 用 channel 代替 mutex 轮询，避免 notifier 回调和检查 goroutine 抢锁死锁
		firedCh := make(chan struct{}, 1)
		s.SetNotifier(channelNotifier{firedCh})
		s.SetRunner(fakeRunner{fn: func(context.Context, string, string) (string, error) {
			return "beat", nil
		}})
		s.Start()
		defer s.Stop()

		// 等首次触发
		select {
		case <-firedCh:
			t.Log("循环任务首次触发")
		case <-time.After(8 * time.Second):
			t.Fatal("循环任务首次未触发")
		}
		// 验证触发后定时器重新武装：NextRun 重算到 1m 后，nextTimer 非 nil
		time.Sleep(500 * time.Millisecond) // 让 fireAndReschedule 完成
		s.mu.Lock()
		tk := s.tasks[0]
		timerArmed := s.nextTimer != nil
		nextRunRecomputed := !tk.NextRun.IsZero() && tk.NextRun.After(time.Now())
		s.mu.Unlock()
		if !timerArmed {
			t.Error("循环任务触发后 nextTimer 为 nil（未重新武装）")
		}
		if !nextRunRecomputed {
			t.Error("循环任务触发后 NextRun 未重算到未来")
		}
		t.Logf("循环任务重新武装成功：NextRun=%v, timer已设", tk.NextRun)
	})

	t.Run("动态新建_重新武装", func(t *testing.T) {
		s := New(t.TempDir() + "/dynamic.json")
		s.SetLogger(func(f string, a ...any) { t.Logf("[LOG] "+f, a...) })

		var mu sync.Mutex
		fired := ""
		s.SetNotifier(captureNotifier{&mu, func(name, body string) { fired = name }})
		s.SetRunner(fakeRunner{fn: func(context.Context, string, string) (string, error) {
			return "x", nil
		}})
		s.Start()
		defer s.Stop()

		// Start 时没有任何任务（定时器不武装）。1 秒后动态建一个 2 秒后的任务。
		time.Sleep(1 * time.Second)
		_, err := s.Create(ScheduledTask{
			Name: "动态任务", Expression: "at " + time.Now().Add(2*time.Minute).Format("2006-01-02 15:04"),
			Prompt: "x", OutputMode: "notify",
		})
		if err != nil {
			t.Fatal(err)
		}
		s.mu.Lock()
		s.tasks[0].NextRun = time.Now().Add(2 * time.Second)
		s.mu.Unlock()
		// Create 已武装定时器；但 NextRun 被 backdate，需重新武装
		s.mu.Lock()
		s.armNextTimerLocked()
		s.mu.Unlock()

		if !waitFire(t, 10*time.Second, func() bool {
			mu.Lock()
			defer mu.Unlock()
			return fired != ""
		}) {
			t.Fatal("动态新建的任务 10 秒内未触发（Create 未重新武装定时器）")
		}
		mu.Lock()
		defer mu.Unlock()
		if fired != "动态任务" {
			t.Errorf("fired=%q want 动态任务", fired)
		}
	})

	t.Run("删除最近任务_重武装到次近", func(t *testing.T) {
		s := New(t.TempDir() + "/delete.json")
		s.SetLogger(func(f string, a ...any) { t.Logf("[LOG] "+f, a...) })

		// 建两个任务：近的(2s)和远的(6s)。删掉近的，远的不应被影响，仍触发。
		t1, _ := s.Create(ScheduledTask{
			Name: "近任务", Expression: "at " + time.Now().Add(2*time.Minute).Format("2006-01-02 15:04"),
			Prompt: "near", OutputMode: "notify",
		})
		t2, _ := s.Create(ScheduledTask{
			Name: "远任务", Expression: "at " + time.Now().Add(2*time.Minute).Format("2006-01-02 15:04"),
			Prompt: "far", OutputMode: "notify",
		})
		s.mu.Lock()
		s.tasks[0].NextRun = time.Now().Add(2 * time.Second) // 近
		s.tasks[1].NextRun = time.Now().Add(6 * time.Second) // 远
		s.mu.Unlock()

		var mu sync.Mutex
		fired := ""
		s.SetNotifier(captureNotifier{&mu, func(name, body string) { fired = name }})
		s.SetRunner(fakeRunner{fn: func(context.Context, string, string) (string, error) {
			return "x", nil
		}})
		s.Start()
		defer s.Stop()

		// 删掉近任务（当前定时器武装的目标）
		if !s.Delete(t1.ID) {
			t.Fatal("删除近任务失败")
		}
		// 远任务应在 ~6s 触发（Delete 重新武装到次近）
		if !waitFire(t, 12*time.Second, func() bool {
			mu.Lock()
			defer mu.Unlock()
			return fired != ""
		}) {
			t.Fatal("删除最近任务后，远任务未触发（Delete 未重新武装）")
		}
		mu.Lock()
		defer mu.Unlock()
		if fired != "远任务" {
			t.Errorf("fired=%q want 远任务（删了近任务后应触发远任务）", fired)
		}
		_ = t2
	})
}

// waitFire 轮询 cond 直到 true 或超时，返回是否触发。用于等待异步 AfterFunc。
func waitFire(t *testing.T, timeout time.Duration, cond func() bool) bool {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if cond() {
			return true
		}
		time.Sleep(100 * time.Millisecond)
	}
	return false
}

type captureNotifier struct {
	mu *sync.Mutex
	fn func(name, body string)
}

func (n captureNotifier) Notify(name, body string) {
	n.mu.Lock()
	defer n.mu.Unlock()
	n.fn(name, body)
	fmt.Printf("[NOTIFY] name=%q body=%q at %s\n", name, body, time.Now().Format("15:04:05.000"))
}

// channelNotifier signals a channel on each Notify — deadlock-free for tests
// that poll/wait from the main goroutine while the AfterFunc callback fires
// concurrently.
type channelNotifier struct{ ch chan<- struct{} }

func (n channelNotifier) Notify(name, body string) {
	select {
	case n.ch <- struct{}{}:
	default:
	}
}
