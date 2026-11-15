package guard

import (
	"regexp"
	"testing"
)

// vouvoiement détecte le vouvoiement dans un texte français : les pronoms
// « vous », « votre », « vos » (et « -vous » collé à un impératif) et les
// impératifs de la deuxième personne du pluriel que l'application emploie.
// La maquette approuvée (docs/waves/design/snapshot.html) tutoie partout, y
// compris dans l'espace organisateur.
var vouvoiement = regexp.MustCompile(`(?i)\b(vous|votre|vos|nous)\b|-vous\b|\b(r[ée]essayez|rechargez|validez|supprimez|ignorez|choisissez|confirmez|consultez|utilisez|patientez|organisez|copiez|renouvelez|identifiez|vérifiez|lancez|voyez|faites|demandez|ajoutez|connectez)\b`)

// TestFrenchCataloguesTutoient : aucune valeur du catalogue français ne vouvoie.
func TestFrenchCataloguesTutoient(t *testing.T) {
	fr := loadCatalogs(t).lang("fr")
	for _, k := range sortedKeys(fr) {
		if m := vouvoiement.FindString(fr[k]); m != "" {
			t.Errorf("%s : « %s » vouvoie (« %s ») ; la maquette tutoie : écrire à la deuxième personne du singulier", k, fr[k], m)
		}
	}
}
