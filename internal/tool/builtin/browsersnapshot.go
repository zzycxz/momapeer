package builtin

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync/atomic"

	"github.com/chromedp/cdproto/accessibility"
	"github.com/chromedp/cdproto/cdp"
	"github.com/chromedp/cdproto/dom"
	"github.com/chromedp/cdproto/runtime"
	"github.com/chromedp/chromedp"
)

// Accessibility-snapshot tools (Phase 1补强). These mirror Playwright MCP's
// proven design: instead of asking the LLM to guess CSS selectors (which fails
// on dynamic class names and is ambiguous), browser_snapshot returns the page's
// accessibility tree with stable element refs (e1, e2, …). The LLM reads
// role+name (semantically meaningful: textbox "用户名", button "登录"), picks a
// ref, and passes it to browser_click / browser_type / browser_select_option.
//
// Refs are per-session and stable only until the page mutates — the snapshot
// must be re-taken after navigation or a DOM-changing action. We store the
// ref→backendNodeId map on the session so the action tools can resolve a ref
// back to the DOM node via dom.ResolveNode + runtime.CallFunctionOn (the same
// mechanism Playwright uses).
//
// Why this matters: CSS selectors are unreliable for LLM-driven automation
// (modern pages hash their classes; the LLM can't see the DOM to construct a
// good selector). Refs are server-assigned and unambiguous, and the
// accessibility tree is text (token-cheap) rather than a screenshot (VLM-only,
// resolution-dependent). Microsoft validated this approach at scale.

// refSeq generates short, human-readable ref ids per session. Reset on each
// snapshot so the numbering stays compact (e1..eN, not e8472).
var refSeq atomic.Int64

// axNodeInfo is the flattened, ref-tagged view of one accessibility node.
type axNodeInfo struct {
	ref       string // "e12"
	role      string // "button", "textbox", "link"
	name      string // accessible name ("登录", "用户名")
	value     string // current value (for inputs)
	backendID cdp.BackendNodeID
}

// snapshotRefs holds the ref→node map from the most recent snapshot. Stored on
// the session (browserSession.refs). Cleared on navigate.
type snapshotRefs map[string]axNodeInfo

// browserSnapshot
type browserSnapshot struct{}

func (browserSnapshot) Name() string { return "browser_snapshot" }

func (browserSnapshot) Description() string {
	return "Capture the page's accessibility tree as text with element refs (e1, e2, …), mirroring Playwright MCP's design. Each interactive element appears as its semantic role + name (e.g. textbox \"用户名\" [ref=e5], button \"登录\" [ref=e3]). Read the tree to find the element you need, then pass its ref to browser_click / browser_type / browser_select_option. Prefer this over CSS selectors — refs are unambiguous and don't require guessing class names. Refs are stable until the page changes; re-snapshot after navigation or DOM mutations. Use browser_screenshot only when you need to see visual layout the accessibility tree can't convey."
}

func (browserSnapshot) Schema() json.RawMessage {
	return json.RawMessage(`{
"type":"object",
"properties":{
  "session_id":{"type":"string"}
},
"required":["session_id"]
}`)
}

func (browserSnapshot) ReadOnly() bool { return true }

func (browserSnapshot) Execute(ctx context.Context, args json.RawMessage) (string, error) {
	var p struct {
		SessionID string `json:"session_id"`
	}
	if err := json.Unmarshal(args, &p); err != nil {
		return "", fmt.Errorf("invalid args: %w", err)
	}
	if p.SessionID == "" {
		return "", errors.New("session_id is required")
	}
	s, err := getBrowserSession(p.SessionID)
	if err != nil {
		return "", err
	}
	// Fetch the full AX tree. chromedp's accessibility domain needs enabling for
	// some events, but GetFullAXTree.Do works without Enable (it's a one-shot
	// query). We enable defensively to populate backendDOMNodeID reliably.
	nodes, err := captureAXTree(s)
	if err != nil {
		return "", fmt.Errorf("snapshot: %w", err)
	}
	// Build the ref map and the rendered tree.
	refs, rendered := buildSnapshotRefs(nodes)
	// Publish atomically — a concurrent navigate clearing refs won't race with
	// this store. snapshotRefs is built fresh above, never mutated, so readers
	// can Load it lock-free.
	published := refs
	s.refs.Store(&published)
	return rendered, nil
}

// captureAXTree runs the AX-tree fetch with the shared action timeout.
func captureAXTree(s *browserSession) ([]*accessibility.Node, error) {
	actx, cancel := context.WithTimeout(s.ctx, browserActionTimeout)
	defer cancel()
	var nodes []*accessibility.Node
	// Enable so backendDOMNodeID is populated; disable after to stop event flow.
	err := chromedp.Run(actx,
		accessibility.Enable(),
		chromedp.ActionFunc(func(ctx context.Context) error {
			var err error
			nodes, err = accessibility.GetFullAXTree().Do(ctx)
			return err
		}),
		accessibility.Disable(),
	)
	return nodes, err
}

// buildSnapshotRefs flattens the AX tree into a ref map and a Playwright-style
// indented text rendering. Only interactive/meaningful nodes get refs (we skip
// pure-structural nodes like generic divs with no name, to keep the tree short
// and refs compact). Returns the ref→info map and the rendered string.
func buildSnapshotRefs(nodes []*accessibility.Node) (snapshotRefs, string) {
	// Reset ref numbering per snapshot.
	refSeq.Store(0)
	// Index by NodeID for parent/child linking.
	byID := make(map[accessibility.NodeID]*accessibility.Node, len(nodes))
	for _, n := range nodes {
		if n == nil {
			continue
		}
		byID[n.NodeID] = n
	}
	refs := make(snapshotRefs, len(nodes))

	// First pass: assign refs to nodes worth surfacing (interactive roles or
	// named nodes). Track depth for rendering.
	// rolesWorthRef: elements the LLM can act on. We assign refs to these; purely
	// structural nodes (none of these roles AND no name) are rendered without a
	// ref or skipped entirely to reduce noise.
	rolesWorthRef := map[string]bool{
		"button": true, "link": true, "textbox": true, "checkbox": true, "radio": true,
		"menuitem": true, "menuitemcheckbox": true, "menuitemradio": true,
		"combobox": true, "listbox": true, "option": true, "tab": true,
		"slider": true, "spinbutton": true, "searchbox": true, "switch": true,
		"treeitem": true, "buttonlink": true,
	}

	// Assign refs to qualifying nodes.
	for _, n := range nodes {
		if n == nil || n.Ignored {
			continue
		}
		role := axValueString(n.Role)
		name := axValueString(n.Name)
		if rolesWorthRef[role] || name != "" {
			ref := fmt.Sprintf("e%d", refSeq.Add(1))
			info := axNodeInfo{
				ref:       ref,
				role:      role,
				name:      name,
				value:     axValueString(n.Value),
				backendID: n.BackendDOMNodeID,
			}
			refs[ref] = info
		}
	}

	// Render: walk from root (no parent) indented, showing role + name + ref.
	var b strings.Builder
	b.WriteString("- accessibility tree (use refs in browser_click/browser_type/browser_select_option):\n")
	var render func(n *accessibility.Node, depth int)
	render = func(n *accessibility.Node, depth int) {
		if n == nil || n.Ignored {
			return
		}
		role := axValueString(n.Role)
		name := axValueString(n.Name)
		value := axValueString(n.Value)
		// Skip pure-noise structural nodes (no role, no name, no value).
		if role == "" && name == "" && value == "" {
			for _, cid := range n.ChildIDs {
				render(byID[cid], depth)
			}
			return
		}
		// Find this node's ref (if it got one).
		var ref string
		for r, info := range refs {
			if info.backendID == n.BackendDOMNodeID && info.backendID != 0 {
				ref = r
				break
			}
		}
		indent := strings.Repeat("  ", depth)
		line := indent + "- " + role
		if name != "" {
			line += fmt.Sprintf(" %q", name)
		}
		if value != "" {
			line += fmt.Sprintf(" (value: %q)", truncate(value, 40))
		}
		if ref != "" {
			line += fmt.Sprintf(" [ref=%s]", ref)
		}
		b.WriteString(line + "\n")
		for _, cid := range n.ChildIDs {
			render(byID[cid], depth+1)
		}
	}
	// Roots are nodes with no parent referenced in the tree.
	roots := findRoots(nodes, byID)
	for _, r := range roots {
		render(r, 1)
	}
	return refs, b.String()
}

// findRoots returns nodes whose ParentID is empty or points to a node not in the
// set (the document root(s)).
func findRoots(nodes []*accessibility.Node, byID map[accessibility.NodeID]*accessibility.Node) []*accessibility.Node {
	var roots []*accessibility.Node
	for _, n := range nodes {
		if n == nil {
			continue
		}
		if n.ParentID == "" || byID[n.ParentID] == nil {
			roots = append(roots, n)
		}
	}
	// Stable order for deterministic output.
	sort.Slice(roots, func(i, j int) bool { return roots[i].NodeID < roots[j].NodeID })
	return roots
}

// axValueString extracts the human-readable string from an accessibility.Value.
// The Value.Value field is raw JSON (jsontext.Value); for role/name/value it's
// typically a JSON string. We unmarshal defensively.
func axValueString(v *accessibility.Value) string {
	if v == nil {
		return ""
	}
	raw := v.Value
	if len(raw) == 0 {
		return ""
	}
	// Try string first (common case for role/name).
	var s string
	if json.Unmarshal(raw, &s) == nil {
		return strings.TrimSpace(s)
	}
	// Fall back to raw text for non-string values.
	return strings.TrimSpace(string(raw))
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}

// resolveRefToObjectID resolves a snapshot ref to a JS RemoteObjectID via the
// stored backendNodeId. This is the bridge between "ref e5" and an actual DOM
// node the action can call functions on (click, set value, etc.) — the same
// dom.ResolveNode + runtime.CallFunctionOn path Playwright uses.
// ctx is the caller's turn context; resolution respects its cancellation and
// is additionally bounded by browserActionTimeout.
func resolveRefToObjectID(ctx context.Context, s *browserSession, ref string) (runtime.RemoteObjectID, error) {
	refsPtr := s.refs.Load()
	if refsPtr == nil {
		return "", fmt.Errorf("no snapshot taken for session %q — call browser_snapshot first to get refs", s.id)
	}
	info, ok := (*refsPtr)[ref]
	if !ok {
		return "", fmt.Errorf("ref %q not found in the last snapshot (refs expire when the page changes; re-run browser_snapshot)", ref)
	}
	if info.backendID == 0 {
		return "", fmt.Errorf("ref %q has no DOM node (it may be a virtual node like a list container); target a concrete element", ref)
	}
	parent := s.ctx
	if ctx != nil {
		parent = ctx
	}
	actx, cancel := context.WithTimeout(parent, browserActionTimeout)
	defer cancel()
	var objID runtime.RemoteObjectID
	err := chromedp.Run(actx, chromedp.ActionFunc(func(ctx context.Context) error {
		obj, err := dom.ResolveNode().WithBackendNodeID(info.backendID).Do(ctx)
		if err != nil {
			return err
		}
		if obj == nil || obj.ObjectID == "" {
			return fmt.Errorf("ref %q resolved to no DOM object (page may have changed)", ref)
		}
		objID = obj.ObjectID
		return nil
	}))
	if err != nil {
		return "", err
	}
	return objID, nil
}

// callOnRef resolves a ref to a DOM object and calls a function on it. The
// functionDeclaration receives the element as `this`. Returns the function's
// serialized result. Used by click/type/select to act on snapshot refs.
// ctx is the caller's turn context; propagated to ref resolution and the CDP
// call so a cancelled turn aborts promptly.
func callOnRef(ctx context.Context, s *browserSession, ref string, fnDecl string, args ...any) (string, error) {
	objID, err := resolveRefToObjectID(ctx, s, ref)
	if err != nil {
		return "", err
	}
	parent := s.ctx
	if ctx != nil {
		parent = ctx
	}
	actx, cancel := context.WithTimeout(parent, browserActionTimeout)
	defer cancel()
	argJSON := make([]*runtime.CallArgument, 0, len(args))
	for _, a := range args {
		b, _ := json.Marshal(a)
		argJSON = append(argJSON, &runtime.CallArgument{Value: b})
	}
	var resultJSON string
	err = chromedp.Run(actx, chromedp.ActionFunc(func(ctx context.Context) error {
		res, ex, err := runtime.CallFunctionOn(fnDecl).
			WithObjectID(objID).
			WithArguments(argJSON).
			WithAwaitPromise(true).
			WithReturnByValue(true).
			Do(ctx)
		if err != nil {
			return err
		}
		if ex != nil {
			// Prefer the exception object's description, fall back to text.
			detail := ex.Text
			if ex.Exception != nil && ex.Exception.Description != "" {
				detail = ex.Exception.Description
			}
			return fmt.Errorf("element function threw: %s", detail)
		}
		if res != nil {
			resultJSON = string(res.Value)
		}
		// Release the remote object to avoid leaking object handles.
		_ = runtime.ReleaseObject(objID).Do(ctx)
		return nil
	}))
	if err != nil {
		return "", err
	}
	return resultJSON, nil
}

// scrollRefIntoView scrolls the ref's element into the viewport. Best-effort:
// click/type call it so coordinates land on the element, but a scroll failure
// is not fatal — the action proceeds regardless. Uses the "nearest" block
// setting to avoid jarring full-page jumps when the element sits inside a
// scroll container.
func scrollRefIntoView(ctx context.Context, s *browserSession, ref string) {
	const js = `function() {
		if (!this || !this.scrollIntoView) return;
		try { this.scrollIntoView({block: "nearest", inline: "nearest"}); } catch (e) {}
	}`
	_, _ = callOnRef(ctx, s, ref, js)
}
