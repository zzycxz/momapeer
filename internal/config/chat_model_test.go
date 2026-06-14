package config

import (
	"reflect"
	"testing"
)

// ── IsLikelyChatModel ──────────────────────────────────────────────────────────

func TestIsLikelyChatModel_RejectsEmptyInput(t *testing.T) {
	for _, model := range []string{"", "   ", "\t"} {
		if IsLikelyChatModel(model) {
			t.Errorf("IsLikelyChatModel(%q) = true, want false", model)
		}
	}
}

func TestIsLikelyChatModel_AllowsKnownChatModels(t *testing.T) {
	for _, model := range []string{
		"jiutian-lan.5", "jiutian-lan-35b", "jiutian-lan-pro", "jiutian-lan-omni",
		"qwen3.6-35b", "qwen3.6-27b",
		"gpt-4o", "gpt-4o-mini",
		"claude-3.5-sonnet", "qwen-max",
	} {
		if !IsLikelyChatModel(model) {
			t.Errorf("IsLikelyChatModel(%q) = false, want true", model)
		}
	}
}

func TestIsLikelyChatModel_FiltersAudioModels(t *testing.T) {
	// Real-world samples from #3483.
	for _, model := range []string{
		"jiutian-lan.5-asr", "jiutian-lan.5-tts", "jiutian-lan.5-tts-voice",
		"jiutian-lan-tts-voiceclone", "jiutian-lan-tts-voicedesign",
		"tts-1",
	} {
		if IsLikelyChatModel(model) {
			t.Errorf("IsLikelyChatModel(%q) = true, want false", model)
		}
	}
}

func TestIsLikelyChatModel_FiltersNonChatKeywords(t *testing.T) {
	for _, model := range []string{
		"whisper-1",
		"text-embedding-3-small", "text-embedding-ada-002",
		"text-moderation-stable",
		"rerank-v1",
		"dall-e-3",
		"text-to-speech-v1", "speech-to-text-v2",
	} {
		if IsLikelyChatModel(model) {
			t.Errorf("IsLikelyChatModel(%q) = true, want false", model)
		}
	}
}

func TestIsLikelyChatModel_DoesNotFilterVoiceAlone(t *testing.T) {
	for _, model := range []string{
		"voice-chat-model", "gpt-4o-voice",
	} {
		if !IsLikelyChatModel(model) {
			t.Errorf("IsLikelyChatModel(%q) = false, want true", model)
		}
	}
}

// ── ModelList / ChatModelList ──────────────────────────────────────────────────

func TestModelList_ReturnsRawList(t *testing.T) {
	p := ProviderEntry{
		Models: []string{
			"jiutian-lan.5", "jiutian-lan-35b",
			"jiutian-lan.5-asr", "jiutian-lan.5-tts", "jiutian-lan.5-tts-voice",
		},
	}
	got := p.ModelList()
	want := []string{
		"jiutian-lan.5", "jiutian-lan-35b",
		"jiutian-lan.5-asr", "jiutian-lan.5-tts", "jiutian-lan.5-tts-voice",
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("ModelList() = %v, want %v", got, want)
	}
}

func TestChatModelList_FiltersNonChatModels(t *testing.T) {
	p := ProviderEntry{
		Models: []string{
			"jiutian-lan.5", "jiutian-lan-35b",
			"jiutian-lan.5-asr", "jiutian-lan.5-tts", "jiutian-lan.5-tts-voice",
			"jiutian-lan-tts-voiceclone", "jiutian-lan-tts-voicedesign",
		},
	}
	got := p.ChatModelList()
	want := []string{"jiutian-lan.5", "jiutian-lan-35b"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("ChatModelList() = %v, want %v", got, want)
	}
}

func TestChatModelList_AllNonChat(t *testing.T) {
	p := ProviderEntry{
		Models: []string{"jiutian-lan.5-tts", "jiutian-lan.5-asr"},
	}
	got := p.ChatModelList()
	if len(got) != 0 {
		t.Errorf("ChatModelList() = %v, want empty", got)
	}
}

func TestChatModelList_AllChat(t *testing.T) {
	p := ProviderEntry{
		Models: []string{"gpt-4o", "gpt-4o-mini"},
	}
	got := p.ChatModelList()
	want := []string{"gpt-4o", "gpt-4o-mini"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("ChatModelList() = %v, want %v", got, want)
	}
}

func TestChatModelList_EmptyModels(t *testing.T) {
	p := ProviderEntry{}
	if got := p.ChatModelList(); got != nil {
		t.Errorf("ChatModelList() = %v, want nil", got)
	}
}
