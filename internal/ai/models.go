package ai

import (
	"regexp"
	"slices"
	"strconv"
	"strings"
)

// Jobs a model is picked for: live tips need a quick answer, reviews the best one.
const (
	JobLive   = "live"
	JobReview = "review"
)

// notChat matches models that make music, pictures, video or speech, or only embed text.
var notChat = regexp.MustCompile(`(?i)(embed|tts|whisper|dall-e|image|imagen|audio|realtime|moderation|transcri|search|guard|vision-exp|computer-use|codex-mini|lyria|veo|music|video|speech|voice|ocr|rerank|customtools|:batch|:free)`)

// The family words must stand on their own in the name: "gemini" is not a "mini" model.
var fastModel = regexp.MustCompile(`(?i)(^|[-_/.:])(flash|mini|lite|haiku|nano|luna|fast|turbo|instant|small|sonnet)([-_/.:]|$)`)

// tinyModel is the smallest of a family: fast, but too weak for coaching advice when a
// middle model exists.
var tinyModel = regexp.MustCompile(`(?i)(^|[-_/.:])(haiku|nano|lite)([-_/.:]|$)`)

var strongModel = regexp.MustCompile(`(?i)(^|[-_/.:])(pro|opus|fable|max|large|astra|ultra)([-_/.:]|$)`)

var premium = regexp.MustCompile(`(?i)(^|[-_/.:])(pro|max|ultra|opus|fable)([-_/.:]|$)`)

func ChatModels(models []Model) []Model {
	return slices.DeleteFunc(slices.Clone(models), func(m Model) bool { return m.ID == "" || notChat.MatchString(m.ID) })
}

// Recommend picks the model for a job from what the provider offers: the newest model of the
// right kind, or the newest overall when none is of that kind.
func Recommend(models []Model, job string) string {
	models = ChatModels(models)
	if len(models) == 0 {
		return ""
	}
	slices.SortStableFunc(models, newerFirst)
	rank := func(id string) int {
		fast, strong, top := fastModel.MatchString(id), strongModel.MatchString(id), premium.MatchString(id)
		tiny := tinyModel.MatchString(id)
		if job == JobLive {
			switch {
			case fast && !tiny && !top:
				return 3
			case fast && !top:
				return 2
			case fast:
				return 1
			}
			return 0
		}
		switch {
		case top && !fast:
			return 3
		case strong && !fast:
			return 2
		case !fast:
			return 1
		}
		return 0
	}
	best := models[0]
	for _, m := range models[1:] {
		if rank(m.ID) > rank(best.ID) {
			best = m
		}
	}
	return best.ID
}

// newerFirst orders by the date the provider gives, then by the version numbers in the name,
// so "gemini-3.8-flash" comes before "gemini-2.5-flash".
func newerFirst(a, b Model) int {
	if a.Created != b.Created {
		if a.Created > b.Created {
			return -1
		}
		return 1
	}
	va, vb := versionOf(a.ID), versionOf(b.ID)
	for i := 0; i < len(va) && i < len(vb); i++ {
		if va[i] != vb[i] {
			if va[i] > vb[i] {
				return -1
			}
			return 1
		}
	}
	if len(va) != len(vb) {
		return len(vb) - len(va)
	}
	return strings.Compare(a.ID, b.ID)
}

var digits = regexp.MustCompile(`\d+`)

// versionOf reads the numbers out of a model name, ignoring long ones that are dates.
func versionOf(id string) []int {
	var out []int
	for _, d := range digits.FindAllString(id, -1) {
		if len(d) > 3 {
			continue
		}
		n, _ := strconv.Atoi(d)
		out = append(out, n)
	}
	return out
}

func Offers(models []Model, id string) bool {
	return slices.ContainsFunc(models, func(m Model) bool { return strings.EqualFold(m.ID, id) })
}
