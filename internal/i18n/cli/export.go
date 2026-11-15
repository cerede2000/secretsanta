package cli

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"sort"

	"secretsanta/internal/i18n/catalog"
)

// Export écrit un fichier plat unique pour la langue : métadonnées, référence
// `_ref` (textes de la langue par défaut) puis toutes les clés de la langue par
// défaut. Les valeurs reprennent la traduction existante, sinon le texte de
// référence (à traduire).
func Export(w io.Writer, cat *catalog.Catalog, lang string) error {
	if !catalog.ValidLangCode(lang) {
		return fmt.Errorf("code de langue invalide : %q", lang)
	}
	def := cat.DefaultLang()
	ref := cat.Messages(def)
	cur := cat.Messages(lang)
	name := lang
	for _, l := range cat.Langs() {
		if l.Code == lang {
			name = l.Name
		}
	}
	keys := cat.Keys(def)
	sort.Strings(keys)

	var b bytes.Buffer
	b.WriteString("{\n")
	meta, _ := json.Marshal(map[string]string{"code": lang, "name": name})
	fmt.Fprintf(&b, "  \"_meta\": %s,\n", meta)
	b.WriteString("  \"_ref\": {\n")
	for i, k := range keys {
		sep := ","
		if i == len(keys)-1 {
			sep = ""
		}
		fmt.Fprintf(&b, "    %s: %s%s\n", quote(k), quote(ref[k]), sep)
	}
	b.WriteString("  }")
	for _, k := range keys {
		v, ok := cur[k]
		if !ok {
			v = ref[k]
		}
		fmt.Fprintf(&b, ",\n  %s: %s", quote(k), quote(v))
	}
	b.WriteString("\n}\n")
	_, err := w.Write(b.Bytes())
	return err
}

func quote(s string) string {
	var b bytes.Buffer
	enc := json.NewEncoder(&b)
	enc.SetEscapeHTML(false)
	_ = enc.Encode(s)
	return string(bytes.TrimRight(b.Bytes(), "\n"))
}
