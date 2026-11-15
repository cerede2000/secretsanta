package review

import (
	"regexp"
	"strings"
	"testing"
	"unicode"
	"unicode/utf8"
)

var bangs = regexp.MustCompile(`!{2,}`)

func TestEnglishMailSubjects(t *testing.T) {
	cat := load(t)
	en := cat.Messages("en")
	n := 0
	for _, k := range sortedKeys(en) {
		if !strings.HasPrefix(k, "mail.") || !strings.HasSuffix(k, "subject") {
			continue
		}
		n++
		s := en[k]
		if utf8.RuneCountInString(s) > 60 {
			t.Errorf("%s: subject longer than 60 characters: %q", k, s)
		}
		if bangs.MatchString(s) {
			t.Errorf("%s: repeated exclamation marks: %q", k, s)
		}
		letters, upper := 0, 0
		for _, r := range s {
			if unicode.IsLetter(r) {
				letters++
				if unicode.IsUpper(r) {
					upper++
				}
			}
		}
		if letters >= 4 && upper == letters {
			t.Errorf("%s: all-caps subject: %q", k, s)
		}
	}
	if n == 0 {
		t.Fatal("no English mail subject found")
	}
}
