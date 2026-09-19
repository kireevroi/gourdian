package ai

import (
	"encoding/json"
	"os"
	"testing"
)

// Real names from OpenRouter's catalogue in September 2026.
var catalogue = []Model{
	{ID: "gpt-5-mini"}, {ID: "gpt-5"}, {ID: "gpt-5.6-luna"}, {ID: "gpt-5.6-luna-pro"},
	{ID: "gpt-6-astra"}, {ID: "gpt-6-astra-pro"}, {ID: "text-embedding-3-large"}, {ID: "gpt-6-astra:batch"},
	{ID: "whisper-1"}, {ID: "tts-1-hd"},
}

func TestRecommendPicksTheNewestOfTheRightKind(t *testing.T) {
	if got := Recommend(catalogue, JobLive); got != "gpt-5.6-luna" {
		t.Errorf("live = %q", got)
	}
	if got := Recommend(catalogue, JobReview); got != "gpt-6-astra-pro" {
		t.Errorf("review = %q", got)
	}
	gemini := []Model{{ID: "gemini-2.5-flash"}, {ID: "gemini-2.5-pro"}, {ID: "gemini-3.8-flash"}, {ID: "gemini-3.7-pro"}}
	if got := Recommend(gemini, JobLive); got != "gemini-3.8-flash" {
		t.Errorf("gemini live = %q", got)
	}
	if got := Recommend(gemini, JobReview); got != "gemini-3.7-pro" {
		t.Errorf("gemini review = %q", got)
	}
}

func TestRecommendTrustsTheProvidersDates(t *testing.T) {
	models := []Model{{ID: "deepseek-chat", Created: 1790000000}, {ID: "deepseek-v3", Created: 1700000000}}
	if got := Recommend(models, JobReview); got != "deepseek-chat" {
		t.Errorf("review = %q", got)
	}
}

func TestClaudeCodeUsesAliases(t *testing.T) {
	models := (&Claude{}).Info().Models
	if got := Recommend(models, JobLive); got != "sonnet" {
		t.Errorf("live = %q", got)
	}
	if got := Recommend(models, JobReview); got != "fable" && got != "opus" {
		t.Errorf("review = %q", got)
	}
}

// TestRecommendOnTheLiveCatalogue checks the picks against a saved copy of OpenRouter's list.
func TestRecommendOnTheLiveCatalogue(t *testing.T) {
	path := os.Getenv("OPENROUTER_MODELS")
	if path == "" {
		t.Skip("set OPENROUTER_MODELS to a saved https://openrouter.ai/api/v1/models")
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var list struct {
		Data []struct {
			ID      string `json:"id"`
			Created int64  `json:"created"`
		} `json:"data"`
	}
	json.Unmarshal(data, &list)
	byVendor := map[string][]Model{}
	for _, m := range list.Data {
		vendor := m.ID[:max(0, len(m.ID)-len(m.ID[indexSlash(m.ID)+1:])-1)]
		byVendor[vendor] = append(byVendor[vendor], Model{ID: m.ID, Created: m.Created})
	}
	for _, v := range []string{"openai", "google", "anthropic", "deepseek", "x-ai", "qwen"} {
		t.Logf("%-10s live %-40s review %s", v, Recommend(byVendor[v], JobLive), Recommend(byVendor[v], JobReview))
	}
}

func indexSlash(s string) int {
	for i := range s {
		if s[i] == '/' {
			return i
		}
	}
	return -1
}
