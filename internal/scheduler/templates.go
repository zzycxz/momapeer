package scheduler

// Template is a predefined, one-click scheduled-task recipe. The UI lists these
// in the "模板" menu; selecting one pre-fills the create form (name/expression/
// prompt/output mode). The user then customizes (e.g. picks a concrete time for
// the meeting reminder) and saves.
//
// Templates intentionally avoid jargon — they map directly to common office
// automations a non-technical user would want, mirroring the WorkBuddy "添加自
// 动化任务" pattern of trigger → action → delivery.
type Template struct {
	ID         string `json:"id"`
	Name       string `json:"name"`
	Category   string `json:"category"`   // "reminder" | "data" | "ops"
	Desc       string `json:"desc"`       // short human description
	Expression string `json:"expression"` // default expression (may be a placeholder the UI fills)
	Prompt     string `json:"prompt"`     // default prompt body
	OutputMode string `json:"output_mode"`
	OutputHint string `json:"output_hint"` // UI hint for OutputDest (e.g. "填写收件人邮箱")
	OneShot    bool   `json:"one_shot,omitempty"`
}

// BuiltinTemplates is the static catalog. Order matters — it's how they appear
// in the UI menu.
var BuiltinTemplates = []Template{
	{
		ID:         "daily_report_reminder",
		Name:       "日报提醒",
		Category:   "reminder",
		Desc:       "每个工作日下班前提醒整理日报并发到团队群",
		Expression: "daily 18:00 Mon-Fri",
		Prompt:     "请整理今日工作日报，按「今日完成 / 明日计划 / 阻塞事项」三段式汇总，简洁列出要点。",
		OutputMode: "notify",
	},
	{
		ID:         "weekly_report_reminder",
		Name:       "周报提醒",
		Category:   "reminder",
		Desc:       "每周五下班前提醒提交周报到指定邮箱",
		Expression: "daily 17:00 Fri",
		Prompt:     "请生成本周工作周报，涵盖本周主要进展、下周计划、风险与求助，语气专业简洁。",
		OutputMode: "email",
		OutputHint: "填写收件人邮箱（可加 ;自定义主题）",
	},
	{
		ID:         "meeting_reminder",
		Name:       "会议提醒",
		Category:   "reminder",
		Desc:       "一次性提醒：某次会议开始前的通知（选一个具体时间）",
		Expression: "at 2026-06-24 14:45",
		Prompt:     "15分钟后有会议，请准备相关材料并准时参加。",
		OutputMode: "notify",
		OneShot:    true,
	},
	{
		ID:         "data_scrape",
		Name:       "定时数据抓取",
		Category:   "data",
		Desc:       "每天早上抓取关键数据并保存为本地文件",
		Expression: "daily 09:00",
		Prompt:     "打开浏览器，抓取昨日关键业务数据（销售/流量/库存），汇总为 CSV 并保存到桌面。",
		OutputMode: "file",
		OutputHint: "填写保存路径，如 C:\\Users\\me\\Desktop\\daily.csv",
	},
	{
		ID:         "system_check",
		Name:       "系统巡检",
		Category:   "ops",
		Desc:       "每小时检查系统状态，异常时通过飞书告警",
		Expression: "every 1h",
		Prompt:     "检查本机磁盘空间、内存占用、关键进程是否存活；发现异常（磁盘>90%、内存>90%、进程缺失）时简要列出问题。",
		OutputMode: "im",
		OutputHint: "填写飞书会话标识，如 feishu:oc_xxx",
	},
}

// Templates returns a copy of the builtin catalog (so callers can't mutate it).
func Templates() []Template {
	out := make([]Template, len(BuiltinTemplates))
	copy(out, BuiltinTemplates)
	return out
}
