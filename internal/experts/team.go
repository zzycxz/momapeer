package experts

// Package experts implements the "专家团" (expert team) multi-model
// collaboration feature: persist named teams of model-backed experts, then run
// them against a task in one of three collaboration modes (parallel / debate /
// pipeline), streaming each expert's output to the UI as it runs.
//
// The orchestrator is model-agnostic: it calls an ExpertRunner (supplied by the
// desktop layer) with (model, systemPrompt, task) and gets back a result. The
// rate-limited provider decorator (provider.RateLimitedProvider) ensures these
// background calls respect the user's [llm] RPM budget alongside main-agent
// traffic — so an expert team can't starve the user's conversation.

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// Expert is one member of a team: a named model with a role/perspective.
type Expert struct {
	Name        string `json:"name"`        // display name, e.g. "批判者"
	Model       string `json:"model"`       // "provider/model" ref, e.g. "deepseek/deepseek-r1"
	Perspective string `json:"perspective"` // role instruction, e.g. "找风险和漏洞"
}

// Team is a reusable roster of experts + default collaboration settings.
type Team struct {
	ID            string   `json:"id"`
	Name          string   `json:"name"`           // "方案评审团"
	Experts       []Expert `json:"experts"`
	DefaultMode   string   `json:"default_mode"`   // "parallel" | "debate" | "pipeline"
	DefaultRounds int      `json:"default_rounds"` // debate rounds (default 2)
}

// Store persists teams to a JSON file (mirroring scheduler.Store's pattern).
type Store struct {
	path string
	mu   sync.Mutex
	teams []Team
}

// NewStore opens (or creates) the team store at path, loading any persisted teams.
func NewStore(path string) (*Store, error) {
	s := &Store{path: path}
	if err := s.load(); err != nil {
		return nil, err
	}
	return s, nil
}

func (s *Store) load() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	b, err := os.ReadFile(s.path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil // first run
		}
		return err
	}
	return json.Unmarshal(b, &s.teams)
}

func (s *Store) save() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := os.MkdirAll(filepath.Dir(s.path), 0o755); err != nil {
		return err
	}
	b, err := json.MarshalIndent(s.teams, "", "  ")
	if err != nil {
		return err
	}
	tmp := s.path + ".tmp"
	if err := os.WriteFile(tmp, b, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, s.path)
}

// Create adds a team, persisting it. Returns the created team (with ID assigned).
func (s *Store) Create(t Team) (Team, error) {
	if t.Name == "" {
		return Team{}, errors.New("team name is required")
	}
	if len(t.Experts) == 0 {
		return Team{}, errors.New("team must have at least one expert")
	}
	if t.ID == "" {
		t.ID = fmt.Sprintf("team_%d", time.Now().UnixNano())
	}
	if t.DefaultMode == "" {
		t.DefaultMode = "debate"
	}
	if t.DefaultRounds <= 0 {
		t.DefaultRounds = 2
	}
	s.mu.Lock()
	s.teams = append(s.teams, t)
	s.mu.Unlock()
	if err := s.save(); err != nil {
		return Team{}, err
	}
	return t, nil
}

// List returns all teams.
func (s *Store) List() []Team {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]Team, len(s.teams))
	copy(out, s.teams)
	return out
}

// Get returns one team by ID.
func (s *Store) Get(id string) (Team, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, t := range s.teams {
		if t.ID == id {
			return t, true
		}
	}
	return Team{}, false
}

// Delete removes a team by ID.
func (s *Store) Delete(id string) bool {
	s.mu.Lock()
	for i, t := range s.teams {
		if t.ID == id {
			s.teams = append(s.teams[:i], s.teams[i+1:]...)
			s.mu.Unlock()
			_ = s.save()
			return true
		}
	}
	s.mu.Unlock()
	return false
}

// Update replaces a team's mutable fields.
func (s *Store) Update(id string, mut func(*Team)) (Team, error) {
	s.mu.Lock()
	for i := range s.teams {
		if s.teams[i].ID == id {
			mut(&s.teams[i])
			if s.teams[i].Name == "" {
				s.mu.Unlock()
				return Team{}, errors.New("team name is required")
			}
			if len(s.teams[i].Experts) == 0 {
				s.mu.Unlock()
				return Team{}, errors.New("team must have at least one expert")
			}
			t := s.teams[i]
			s.mu.Unlock()
			if err := s.save(); err != nil {
				return Team{}, err
			}
			return t, nil
		}
	}
	s.mu.Unlock()
	return Team{}, fmt.Errorf("team %q not found", id)
}

// BuiltinTeams are the seed rosters shown on first run, so the panel isn't
// empty. Users can edit/delete them.
//
// Each roster is designed around an office-automation closed loop with MoMAPeer's
// existing capabilities (docx/xlsx/mindmap/scheduler/email) — this is our
// differentiator vs. single-persona "expert cards": multiple models collaborate
// in parallel/debate/pipeline rather than one model wearing one hat.
//
// Expert names MUST be unique within a team: the frontend CollabStream reducer
// keys streamed chunks by (expertName, round), so duplicate names would
// mis-accumulate into the wrong message.
var BuiltinTeams = []Team{
	{
		ID:   "builtin_review",
		Name: "方案评审团",
		Experts: []Expert{
			{Name: "批判者", Model: "", Perspective: "从风险、可行性、潜在问题角度批判性审视，指出至少 3 个具体风险点"},
			{Name: "建设者", Model: "", Perspective: "从如何改进、优化、落地的角度，给出具体的改进建议"},
		},
		DefaultMode:   "debate",
		DefaultRounds: 2,
	},
	{
		ID:   "builtin_brainstorm",
		Name: "头脑风暴团",
		Experts: []Expert{
			{Name: "创意官", Model: "", Perspective: "提出大胆、创新的想法，不要自我审查"},
			{Name: "务实官", Model: "", Perspective: "从落地难度、成本、时间线角度评估每个想法的可行性"},
		},
		DefaultMode:   "pipeline",
		DefaultRounds: 1,
	},
	// 文档撰写团 — pipeline: 策划→起草→润色校对。配套 docx 生成能力。
	{
		ID:   "builtin_doc",
		Name: "文档撰写团",
		Experts: []Expert{
			{Name: "策划官", Model: "", Perspective: "先明确文档的目标读者、核心信息、结构大纲（标题层级 + 每节要点）。输出一份结构化大纲，不要写正文。"},
			{Name: "撰稿人", Model: "", Perspective: "基于策划官的大纲撰写完整正文。要求：每段一个观点，语言专业简洁，避免空话套话，关键数据要具体。"},
			{Name: "润色校对", Model: "", Perspective: "审阅撰稿人的正文，修正错别字、语病、冗余表达，统一术语和语气，补充必要的过渡句，使全文连贯易读。"},
		},
		DefaultMode:   "pipeline",
		DefaultRounds: 1,
	},
	// 数据分析团 — pipeline: 提问→分析→解读。配套 xlsx 生成能力。
	{
		ID:   "builtin_data",
		Name: "数据分析团",
		Experts: []Expert{
			{Name: "提问官", Model: "", Perspective: "明确本次分析要回答的业务问题、可用的数据字段、分析的维度和粒度。输出一份分析框架（问题 + 维度 + 指标），不写结论。"},
			{Name: "分析师", Model: "", Perspective: "按提问官的框架做分析：列出关键发现、异常值、趋势，给出每个发现的支撑数据。不要泛泛而谈，每个结论都要有数字。"},
			{Name: "结论解读", Model: "", Perspective: "把分析师的发现翻译成业务语言：这些数字意味着什么、有什么风险或机会、建议下一步动作。面向决策者，避免术语堆砌。"},
		},
		DefaultMode:   "pipeline",
		DefaultRounds: 1,
	},
	// 翻译校对团 — pipeline: 译→校→审。多语言办公场景。
	{
		ID:   "builtin_translate",
		Name: "翻译校对团",
		Experts: []Expert{
			{Name: "译者", Model: "", Perspective: "准确翻译原文，保留原意和专业术语。不要润色，忠实于源文本。标出拿不准或一词多义的地方。"},
			{Name: "校对", Model: "", Perspective: "对照原文逐句校对译者的译文：纠正漏译、错译、术语不一致。列出发现的问题清单。"},
			{Name: "审稿", Model: "", Perspective: "在不改变原意的前提下，让译文读起来像母语写就：调整语序、替换生硬表达、统一文风。输出最终定稿。"},
		},
		DefaultMode:   "pipeline",
		DefaultRounds: 1,
	},
	// 会议纪要团 — pipeline: 整理要点→提炼决议→指派行动项。配套 scheduler/email。
	{
		ID:   "builtin_meeting",
		Name: "会议纪要团",
		Experts: []Expert{
			{Name: "要点整理", Model: "", Perspective: "从会议记录/转录中提取讨论的各个议题及各方观点，按议题分组罗列。保留关键争议点，不评判对错。"},
			{Name: "决议提炼", Model: "", Perspective: "在要点整理的基础上，明确标注哪些议题达成了决议、决议内容是什么、哪些悬而未决需要后续跟进。不要遗漏任何已定事项。"},
			{Name: "行动项指派", Model: "", Perspective: "把每个决议拆解成可执行的行动项：谁负责、做什么、什么时间节点交付。以表格或清单形式输出，确保每项都有明确 owner 和 deadline。"},
		},
		DefaultMode:   "pipeline",
		DefaultRounds: 1,
	},
	// 项目规划团 — debate 2 轮: 进度官↔风险官↔资源官 互相质询。
	{
		ID:   "builtin_project",
		Name: "项目规划团",
		Experts: []Expert{
			{Name: "进度官", Model: "", Perspective: "从时间线和里程碑角度规划：拆解工作分解结构（WBS）、排定关键路径、标注各阶段起止时间。质疑其他专家的排期是否现实。"},
			{Name: "风险官", Model: "", Perspective: "从风险角度审视：技术风险、依赖风险、人员风险各有哪些，概率和影响多大，缓解措施是什么。质疑进度官的乐观假设。"},
			{Name: "资源官", Model: "", Perspective: "从人力/预算/工具角度评估：需要的资源是否到位、瓶颈在哪、如何调配。指出进度和风险评估中遗漏的资源约束。"},
		},
		DefaultMode:   "debate",
		DefaultRounds: 2,
	},
	// 邮件撰写团 — pipeline: 明确目的→起草→调语气。配套 email 推送。
	{
		ID:   "builtin_email",
		Name: "邮件撰写团",
		Experts: []Expert{
			{Name: "目的官", Model: "", Perspective: "先厘清这封邮件要达成什么：是通知、请求、说服还是致歉？收件人是谁、读完该做什么？输出邮件的目标 + 3 个必须传达的要点。"},
			{Name: "起草人", Model: "", Perspective: "按目的官的框架起草邮件正文：主题行简明有力，开头直入主题，正文覆盖三个要点，结尾有明确的行动召唤。"},
			{Name: "语气调整", Model: "", Perspective: "根据收件人关系（上级/平级/客户/外部）调整语气和措辞：正式程度、敬语、缓和或强化的表达。确保专业且得体，不卑不亢。"},
		},
		DefaultMode:   "pipeline",
		DefaultRounds: 1,
	},
}
