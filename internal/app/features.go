package app

import "secretsanta/internal/groups"

// features liste les fonctionnalités branchées : UNE ligne par module.
// Pas encore branchés (leurs ports ou leurs phases arrivent plus tard) :
// access/magic (Directory, LinkFunc, Composer), access/token (pas de magasin SQL).
var features = []func(*App) error{
	registerGroups,
}

// registerGroups construit le service des groupes (routes métier à venir).
func registerGroups(a *App) error {
	s, err := groups.NewService(a.DB, a.Clock, a.Cfg.InstancePass)
	if err != nil {
		return err
	}
	a.Groups = s
	return nil
}
