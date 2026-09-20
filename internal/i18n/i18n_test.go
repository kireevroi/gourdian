package i18n

import (
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"
)

// A Russian phrase must take the same values in the same order as its English.
func TestPhrasesTakeTheSameValues(t *testing.T) {
	verbs := regexp.MustCompile(`%[-+ 0-9.]*[a-z%]`)
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
