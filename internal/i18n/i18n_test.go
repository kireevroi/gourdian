package i18n

import (
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"testing"
)

// A Russian phrase must take the same values in the same order as its English.
func TestPhrasesTakeTheSameValues(t *testing.T) {
	// Go's verbs, not any letter: "54% at Archon" is text, not a %a that has to appear in both.
	verbs := regexp.MustCompile(`%[-+ #0-9.\[\]]*[vTtbcdoOqxXUeEfFgGsp%]`)
	for en, ru := range phrases {
		if a, b := verbs.FindAllString(en, -1), verbs.FindAllString(ru, -1); !slices.Equal(a, b) {
			t.Errorf("%q takes %v, its Russian %q takes %v", en, a, ru, b)
		}
	}
}

// Every Russian text the trainer ships (the Go code and the dashboard's ru.json) uses the
// glossary's wording for game terms.
func TestTheGlossaryIsFollowed(t *testing.T) {
	var texts []string
	filepath.WalkDir("..", func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() || strings.HasSuffix(path, "_test.go") || path == filepath.Join("..", "i18n", "glossary.go") {
			return err
		}
		if strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "ru.json") {
			texts = append(texts, path)
		}
		return nil
	})
	if len(texts) < 20 {
		t.Fatalf("found only %d files to check", len(texts))
	}
	for _, term := range Glossary {
		for _, not := range term.Not {
			// Go's \b is ASCII-only, so a word start is anything but a letter before it.
			re := regexp.MustCompile(`(?i)(^|[^\p{L}])` + regexp.QuoteMeta(not))
			for _, path := range texts {
				data, err := os.ReadFile(path)
				if err != nil {
					t.Fatal(err)
				}
				for i, line := range strings.Split(string(data), "\n") {
					if re.MatchString(line) {
						t.Errorf("%s:%d says %q; %s is %q in Russian", path, i+1, not, term.EN, term.RU)
					}
				}
			}
		}
	}
}

func TestWordAndSay(t *testing.T) {
	if got := Word("ru", "Tormentor"); got != "Торментор" {
		t.Errorf("Word = %q", got)
	}
	if got := Word("en", "Tormentor"); got != "Tormentor" {
		t.Errorf("English Word = %q", got)
	}
	if got := Say("ru", "Goal: %s · %d/%d this week", "x", 1, 3); got != "Цель: x · 1/3 за неделю" {
		t.Errorf("Say = %q", got)
	}
}

// Every English line the trainer looks a translation up for has one. Word and Say hand back
// the English when a phrase is missing, which is how a Russian player came to be briefed in
// English before the horn: the code read like it translated, and silently didn't.
func TestEveryLookedUpLineHasRussian(t *testing.T) {
	// The single-language localisers. Others, like the coach's sources, carry both languages
	// at the call site and need nothing here.
	call := regexp.MustCompile(`(?:\bl\.f\(|\bl\.s\(|\broleSay\(\s*\w+,\s*|\blaneIn\(\s*\w+,\s*|i18n\.Say\(\s*\w+,\s*|i18n\.Word\(\s*\w+,\s*)\s*"((?:[^"\\]|\\.)*)"\s*[,)]`)
	var checked int
	err := filepath.WalkDir("..", func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return err
		}
		data, readErr := os.ReadFile(path)
		if readErr != nil {
			return readErr
		}
		for i, line := range strings.Split(string(data), "\n") {
			for _, m := range call.FindAllStringSubmatch(line, -1) {
				text, err := strconv.Unquote(`"` + m[1] + `"`)
				if err != nil {
					continue
				}
				checked++
				if _, ok := phrases[text]; !ok {
					t.Errorf("%s:%d asks for a translation of %q, which phrases has none for", path, i+1, text)
				}
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if checked < 30 {
		t.Fatalf("only %d lines checked; the scan is looking in the wrong place", checked)
	}
}
