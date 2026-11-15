package people

import "os"

// goSources donne accès aux sources du paquet pour les tests de clés.
var goSources = os.DirFS(".")
