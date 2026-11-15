package suggestionpages

import "os"

func readPagesGo() (string, error) {
	b, err := os.ReadFile("pages.go")
	return string(b), err
}
