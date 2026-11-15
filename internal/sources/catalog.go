package sources

import (
	"context"
	_ "embed"
	"encoding/json"
)

//go:embed data/catalog.json
var catalogJSON []byte

type catalogEntry struct {
	Title       string `json:"title"`
	Description string `json:"description"`
}

// Catalog est le catalogue de secours embarqué : sans réseau ni clé.
type Catalog struct {
	data map[string]map[string][]catalogEntry
}

// NewCatalog charge le catalogue embarqué.
func NewCatalog() *Catalog {
	var d map[string]map[string][]catalogEntry
	if err := json.Unmarshal(catalogJSON, &d); err != nil {
		panic("catalogue embarqué invalide : " + err.Error())
	}
	return &Catalog{data: d}
}

// Name identifie la source.
func (c *Catalog) Name() string { return "catalogue" }

// Search renvoie les entrées de la tranche (ou toutes gammes à défaut).
func (c *Catalog) Search(_ context.Context, q Query) ([]Suggestion, error) {
	bands := c.data[q.lang()]
	entries := bands[string(q.Band)]
	if len(entries) == 0 {
		entries = bands[string(BandAll)]
	}
	out := make([]Suggestion, 0, len(entries))
	for _, e := range entries {
		out = append(out, Suggestion{Title: e.Title, Description: e.Description, Source: c.Name()})
	}
	if n := q.limit(); len(out) > n {
		out = out[:n]
	}
	return out, nil
}
