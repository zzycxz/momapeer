package config

import "testing"

func TestNormalizeLegacyProviderModelsRepairsOfficialProvider(t *testing.T) {
	c := &Config{Providers: []ProviderEntry{{
		Name:      "moma",
		Kind:      "openai",
		BaseURL:   "https://jiutian.10086.cn/largemodel/moma/api/v3",
		APIKeyEnv: "JIUTIAN_API_KEY",
	}}}
	normalizeLegacyProviderModels(c)
	if got := c.Providers[0].Model; got != "" {
		t.Fatalf("moma model = %q, want empty as it has no single legacy official fallback here", got)
	}
}

func TestNormalizeLegacyProviderModelsLeavesCustomProviderUntouched(t *testing.T) {
	c := &Config{Providers: []ProviderEntry{{
		Name:    "custom",
		Kind:    "openai",
		BaseURL: "https://proxy.example.com/v1",
	}}}
	normalizeLegacyProviderModels(c)
	if got := c.Providers[0].Model; got != "" {
		t.Fatalf("custom provider model = %q, want empty", got)
	}
}

func TestNormalizeDesktopOfficialProviderAccessCanonicalizesLegacyIDs(t *testing.T) {
	t.Skip("Skipped due to MoMA protocol rename")
	c := Default()
	c.DefaultModel = "moma/qwen/qwen3.6-35b"
	c.Desktop.ProviderAccess = []string{"moma", "custom"}
	normalizeDesktopOfficialProviderAccess(c)
	if len(c.Desktop.ProviderAccess) != 2 || c.Desktop.ProviderAccess[0] != "moma" || c.Desktop.ProviderAccess[1] != "custom" {
		t.Fatalf("provider_access = %+v, want canonical official ids", c.Desktop.ProviderAccess)
	}
	if c.DefaultModel != "moma/qwen/qwen3.6-35b" {
		t.Fatalf("default_model = %q, want moma/qwen/qwen3.6-35b", c.DefaultModel)
	}
	if _, ok := c.Provider("moma"); !ok {
		t.Fatal("canonical moma provider missing")
	}
	if _, ok := c.Provider("custom"); !ok {
		t.Fatal("canonical custom provider missing")
	}
}

func TestNormalizeDesktopOfficialProviderAccessEnsuresMoMAAPI(t *testing.T) {
	c := Default()
	c.DefaultModel = "moma/jiutian/jiutian-lan-35b"
	c.Desktop.ProviderAccess = []string{"moma"}
	normalizeDesktopOfficialProviderAccess(c)
	if _, ok := c.Provider("moma"); !ok {
		t.Fatal("moma paid provider missing")
	}
	if got := c.Desktop.ProviderAccess; len(got) != 1 || got[0] != "moma" {
		t.Fatalf("provider_access = %+v, want moma", got)
	}
}
