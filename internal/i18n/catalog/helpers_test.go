package catalog

import (
	"bytes"
	"log/slog"
	"strings"
	"sync"
	"testing/fstest"
)

type lockedBuf struct {
	mu sync.Mutex
	b  bytes.Buffer
}

func (l *lockedBuf) Write(p []byte) (int, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.b.Write(p)
}

func (l *lockedBuf) String() string {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.b.String()
}

func (l *lockedBuf) Count(s string) int { return strings.Count(l.String(), s) }

func testLogger() (*slog.Logger, *lockedBuf) {
	buf := &lockedBuf{}
	return slog.New(slog.NewTextHandler(buf, nil)), buf
}

func baseFS() fstest.MapFS {
	return fstest.MapFS{
		"static/i18n/core/fr.json":            {Data: []byte(`{"_meta":{"code":"fr","name":"Français"},"core.hello":"Bonjour {name}","core.only_fr":"Seulement"}`)},
		"static/i18n/core/en.json":            {Data: []byte(`{"_meta":{"code":"en","name":"English"},"core.hello":"Hello {name}"}`)},
		"static/i18n/groups/fr.json":          {Data: []byte(`{"groups.name_required":"Nom requis"}`)},
		"static/i18n/groups/en.json":          {Data: []byte(`{"groups.name_required":"Name required"}`)},
		"static/i18n/admin/dashboard/fr.json": {Data: []byte(`{"admin.dashboard.title":"Tableau"}`)},
		"static/i18n/admin/dashboard/en.json": {Data: []byte(`{"admin.dashboard.title":"Dashboard"}`)},
	}
}
