package skillstore

import (
	"context"
	"testing"
	"time"
)

const testBaseURL = "https://clawhub.ai/api/v1"

func TestListSkills(t *testing.T) {
	c := NewClient(testBaseURL)
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	result, err := c.ListSkills(ctx, ListOptions{Limit: 3, Sort: "stars"})
	if err != nil {
		t.Fatalf("ListSkills: %v", err)
	}
	if len(result.Items) == 0 {
		t.Fatal("ListSkills: expected non-empty items")
	}
	for _, item := range result.Items {
		if item.Slug == "" {
			t.Error("ListSkills: item missing slug")
		}
		if item.DisplayName == "" {
			t.Error("ListSkills: item missing displayName")
		}
	}
	t.Logf("got %d skills, first: %s (⭐ %d)", len(result.Items), result.Items[0].DisplayName, result.Items[0].Stats.Stars)
}

func TestSearchSkills(t *testing.T) {
	c := NewClient(testBaseURL)
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	result, err := c.SearchSkills(ctx, "code review", SearchOptions{Limit: 3})
	if err != nil {
		t.Fatalf("SearchSkills: %v", err)
	}
	if len(result.Results) == 0 {
		t.Fatal("SearchSkills: expected non-empty results")
	}
	t.Logf("got %d results, first: %s (score %.2f)", len(result.Results), result.Results[0].DisplayName, result.Results[0].Score)
}

func TestGetSkillDetail(t *testing.T) {
	c := NewClient(testBaseURL)
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	detail, err := c.GetSkillDetail(ctx, "self-improving-agent")
	if err != nil {
		t.Fatalf("GetSkillDetail: %v", err)
	}
	if detail.Skill.Slug == "" {
		t.Fatal("GetSkillDetail: missing slug")
	}
	if detail.Owner == nil {
		t.Log("GetSkillDetail: no owner info (ok)")
	} else {
		t.Logf("owner: %s", detail.Owner.Handle)
	}
	if detail.LatestVersion != nil {
		t.Logf("latest version: %s", detail.LatestVersion.Version)
	}
}
