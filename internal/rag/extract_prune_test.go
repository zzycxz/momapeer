package rag

import (
	"testing"
)

// TestPruneDanglingRelations proves relations pointing at entities the LLM never
// extracted are dropped before upsert, keeping the graph free of edges to
// non-existent nodes. This is the Go port of HE's _prune_dangling_edges.
func TestPruneDanglingRelations(t *testing.T) {
	res := ExtractResult{
		Entities: []Entity{
			{NameRaw: "张三"},
			{NameRaw: "MoMAPeer"},
		},
		Relations: []Relation{
			{Source: "张三", Target: "MoMAPeer", Type: "负责"},   // valid
			{Source: "张三", Target: "幻觉实体", Type: "虚构"},       // dangling target
			{Source: "虚构公司", Target: "MoMAPeer", Type: "投资"}, // dangling source
		},
	}
	got := pruneDanglingRelations(res)
	if len(got) != 1 {
		t.Fatalf("expected 1 valid relation, got %d: %+v", len(got), got)
	}
	if got[0].Target != "MoMAPeer" {
		t.Errorf("surviving relation wrong: %+v", got[0])
	}
}

// TestPruneDanglingRelationsCaseInsensitive confirms the prune compares by
// normalized name so a casing difference between the entity name and the
// relation endpoint does NOT cause a false drop.
func TestPruneDanglingRelationsCaseInsensitive(t *testing.T) {
	res := ExtractResult{
		Entities: []Entity{{NameRaw: "OpenAI"}, {NameRaw: "GPT-4"}},
		Relations: []Relation{
			{Source: "openai", Target: "gpt-4", Type: "develops"}, // different case, both valid
		},
	}
	got := pruneDanglingRelations(res)
	if len(got) != 1 {
		t.Fatalf("case-different endpoint should NOT be pruned, got %d: %+v", len(got), got)
	}
}

// TestPruneAllDanglingWhenNoEntities confirms that if no entities were
// extracted, every relation is dropped (nothing valid to point at).
func TestPruneAllDanglingWhenNoEntities(t *testing.T) {
	res := ExtractResult{
		Relations: []Relation{{Source: "a", Target: "b", Type: "r"}},
	}
	if got := pruneDanglingRelations(res); len(got) != 0 {
		t.Errorf("with no entities all relations should be pruned, got %d", len(got))
	}
}

// TestParseNodesJSON confirms stage-1 (entities-only) parsing works.
func TestParseNodesJSON(t *testing.T) {
	res, err := parseNodesJSON([]byte(`{"entities":[{"name":"张三","type":"person","description":"总监"}]}`))
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Entities) != 1 || res.Entities[0].NameRaw != "张三" {
		t.Fatalf("parsed wrong: %+v", res)
	}
	if len(res.Relations) != 0 {
		t.Errorf("nodes-only parse should yield no relations, got %d", len(res.Relations))
	}
}

// TestParseRelationsJSON confirms stage-2 (relations-only) parsing works.
func TestParseRelationsJSON(t *testing.T) {
	rels, err := parseRelationsJSON([]byte(`{"relations":[{"source":"张三","target":"项目","type":"负责","description":"主导"}]}`))
	if err != nil {
		t.Fatal(err)
	}
	if len(rels) != 1 || rels[0].Source != "张三" || rels[0].Type != "负责" {
		t.Fatalf("parsed wrong: %+v", rels)
	}
}

// TestFormatKnownNodes confirms the stage-2 known-nodes bullet list is built
// from the stage-1 entities (raw display names, verbatim-matchable).
func TestFormatKnownNodes(t *testing.T) {
	got := formatKnownNodes([]Entity{{NameRaw: "张三"}, {NameRaw: " MoMAPeer "}})
	if got != "- 张三\n- MoMAPeer" {
		t.Errorf("known-nodes list wrong: %q", got)
	}
	if formatKnownNodes(nil) != "（本段未识别到具体实体）" {
		t.Error("empty known-nodes should have the placeholder text")
	}
}
