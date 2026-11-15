package ports

import (
	"context"
	"database/sql"
	"errors"
	"sync"
)

// RuleKind est la nature d'une règle proposée.
type RuleKind string

const (
	RuleExclusion  RuleKind = "exclusion"
	RuleHousehold  RuleKind = "foyer"
	RuleForcing    RuleKind = "forcage"
	RuleRoleChange RuleKind = "role"
)

// AllRuleKinds liste les quatre natures de règle.
var AllRuleKinds = []RuleKind{RuleExclusion, RuleHousehold, RuleForcing, RuleRoleChange}

// ProposedRule est la règle proposée. Les personnes sont désignées par identifiant.
type ProposedRule struct {
	Kind    RuleKind
	Members []int64
}

// RepairPreview ne contient que des comptes, jamais un nom.
type RepairPreview struct {
	AffectedCount          int
	AlreadyDiscoveredCount int
	Repairable             bool
}

// RuleWriter écrit la règle (F3) dans la transaction fournie.
type RuleWriter func(ctx context.Context, tx *sql.Tx) error

// RuleChangeRepair adapte le tirage à une règle modifiée (implémenté par F4, consommé par F3).
type RuleChangeRepair interface {
	Preview(ctx context.Context, groupID int64, rule ProposedRule) (RepairPreview, error)
	// Apply exécute dans une même transaction l'écriture de la règle et la réparation.
	Apply(ctx context.Context, groupID int64, rule ProposedRule, write RuleWriter) error
}

// FakeRuleChangeRepair est une doublure transactionnelle de RuleChangeRepair.
type FakeRuleChangeRepair struct {
	mu sync.Mutex
	// Previews donne l'aperçu par nature de règle.
	Previews map[RuleKind]RepairPreview
	// RepairErr, s'il est défini, fait échouer la réparation.
	RepairErr error
	// Committed contient les règles validées (rien en cas d'échec).
	Committed []ProposedRule
	// RepairCalls compte les réparations tentées.
	RepairCalls int
}

// Preview renvoie l'aperçu configuré ou un aperçu vide réparable.
func (f *FakeRuleChangeRepair) Preview(_ context.Context, _ int64, rule ProposedRule) (RepairPreview, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if p, ok := f.Previews[rule.Kind]; ok {
		return p, nil
	}
	return RepairPreview{Repairable: true}, nil
}

// Apply écrit la règle puis répare ; l'échec de l'un annule l'autre.
func (f *FakeRuleChangeRepair) Apply(ctx context.Context, _ int64, rule ProposedRule, write RuleWriter) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if write == nil {
		return errors.New("écriture de règle absente")
	}
	if err := write(ctx, nil); err != nil {
		return err
	}
	f.RepairCalls++
	if f.RepairErr != nil {
		return f.RepairErr
	}
	f.Committed = append(f.Committed, rule)
	return nil
}
