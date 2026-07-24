package control

import (
	"context"
	"regexp"
	"strings"
	"time"
	"unicode/utf8"
)

const (
	AutoPlanOff = "off"
	AutoPlanOn  = "on"
)

// keep lowercase aliases for internal use
const (
	autoPlanOff = AutoPlanOff
	autoPlanOn  = AutoPlanOn
)

// planApprovalTool aliases the plan-approval Tool name so test fixtures in this
// package can reference it without reaching across to the exported constant.
var planApprovalTool = PlanApprovalTool

var numberedListRE = regexp.MustCompile(`(?m)^\s*(?:[-*]|\d+[.)])\s+\S`)

type autoPlanClassifier interface {
	NeedsPlan(ctx context.Context, input string, score int) (bool, string, error)
}

// NormalizeAutoPlan is the single source of truth for auto_plan normalization.
// Exported so boot.go and config consumers share the same semantics.
func NormalizeAutoPlan(mode string) string {
	return normalizeAutoPlan(mode)
}

func normalizeAutoPlan(mode string) string {
	switch strings.ToLower(strings.TrimSpace(mode)) {
	case autoPlanOn, "ask": // "ask" is a legacy synonym for on.
		return autoPlanOn
	case "", autoPlanOff:
		return autoPlanOff
	default:
		return autoPlanOff
	}
}

func (c *Controller) maybeAutoPlan(ctx context.Context, input string) {
	if c.shouldAutoPlan(ctx, input) {
		c.SetPlanMode(true)
		c.notice("auto plan: task looks multi-step; drafting a plan first")
	}
}

func (c *Controller) shouldAutoPlan(ctx context.Context, input string) bool {
	c.mu.Lock()
	mode := c.autoPlan
	plan := c.planMode
	goalActive := strings.TrimSpace(c.goal) != "" && c.goalStatus == GoalStatusRunning
	classifier := c.classifier
	c.mu.Unlock()
	if mode == autoPlanOff || plan || goalActive {
		return false
	}
	score := autoPlanScore(input)
	if score <= 0 {
		return false
	}
	if classifier != nil && score <= 3 {
		ctx, cancel := context.WithTimeout(ctx, 3*time.Second)
		defer cancel()
		needsPlan, reason, err := classifier.NeedsPlan(ctx, input, score)
		if err == nil {
			if needsPlan && reason != "" {
				c.notice("auto plan classifier: " + reason)
			}
			return needsPlan
		}
		c.notice("auto plan classifier failed; falling back to heuristic: " + err.Error())
	}
	// Heuristic fallback. Was >= 2, but that over-triggered: any two weak shape
	// signals (long text + a couple of newlines, or a pasted log with two .js
	// mentions) fired plan mode. >= 3 requires a stronger cluster of signals,
	// reducing false positives on discussion/explain prompts.
	return score >= 3
}

func autoPlanScore(input string) int {
	text := strings.TrimSpace(input)
	if text == "" || strings.HasPrefix(text, "/") || strings.HasPrefix(text, PlanModeMarker) {
		return 0
	}
	lower := strings.ToLower(text)
	if isLowRiskQuestion(lower) {
		return 0
	}

	score := 0
	if utf8.RuneCountInString(text) >= 160 {
		score++
	}
	if numberedListRE.MatchString(text) {
		score++
	}
	if strings.Count(text, "\n") >= 2 {
		score++
	}
	if containsAny(lower, complexIntentTerms) {
		score++
	}
	if containsAny(lower, multiSurfaceTerms) {
		score++
	}
	if containsAny(lower, docsAndIssueTerms) {
		score++
	}
	if strings.Count(text, "@") >= 2 || strings.Count(lower, ".go")+
		strings.Count(lower, ".tsx")+
		(strings.Count(lower, ".ts")-strings.Count(lower, ".tsx"))+ // ".ts" is a substring of ".tsx", so each .tsx file is counted once by the .tsx term AND once by the .ts term; subtract the overlap so a .tsx file counts once, not twice.
		strings.Count(lower, ".js") >= 2 {
		score++
	}
	return score
}

func isLowRiskQuestion(lower string) bool {
	lower = strings.TrimSpace(lower)
	// Directives that ask for an action (run/show) are low-risk even if they
	// contain a complex-intent word, because the user is asking to execute one
	// concrete thing, not to plan multi-step work.
	if strings.HasPrefix(lower, "运行") || strings.HasPrefix(lower, "run ") ||
		strings.HasPrefix(lower, "show ") {
		return true
	}
	// Explain/discuss/evaluate prompts are low-risk UNLESS they pair with a
	// hard action verb ("实现", "重构", "迁移"…). "解释一下重构方案" is
	// discussion; "重构一下这个模块" is work. The verb list is complexIntentTerms.
	if strings.HasPrefix(lower, "解释") || strings.HasPrefix(lower, "说明") ||
		strings.HasPrefix(lower, "怎么看") || strings.HasPrefix(lower, "查一下") ||
		strings.HasPrefix(lower, "分析") || strings.HasPrefix(lower, "评估") ||
		strings.HasPrefix(lower, "讨论") || strings.HasPrefix(lower, "对比") ||
		strings.HasPrefix(lower, "what ") || strings.HasPrefix(lower, "why ") ||
		strings.HasPrefix(lower, "how ") || strings.HasPrefix(lower, "explain ") ||
		strings.HasPrefix(lower, "analyze") || strings.HasPrefix(lower, "compare") {
		return !containsAny(lower, complexIntentTerms)
	}
	return false
}

func containsAny(s string, terms []string) bool {
	for _, term := range terms {
		if strings.Contains(s, term) {
			return true
		}
	}
	return false
}

var complexIntentTerms = []string{
	"implement", "add support", "refactor", "migrate", "redesign", "end-to-end",
	"e2e", "wire up", "integration", "fix the issue", "build a",
	"实现", "新增", "支持", "重构", "迁移", "改造", "端到端", "联调", "接入",
	"修复这个问题", "修一下这个问题", "补齐", "设计",
}

var multiSurfaceTerms = []string{
	"multiple files", "several files", "across", "frontend", "backend", "config",
	"tests", "docs", "ui", "api", "database", "schema",
	"多个文件", "多处", "前端", "后端", "配置", "测试", "文档", "接口", "数据库",
}

var docsAndIssueTerms = []string{
	"prd", "issue", "requirements", "spec", "proposal", "roadmap",
	"需求", "产品文档", "接口文档", "方案", "规划",
}
