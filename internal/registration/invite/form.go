package invite

import (
	"context"
	"net/mail"
	"strings"
	"unicode/utf8"

	"secretsanta/internal/participants"
	"secretsanta/internal/shared/errs"
	"secretsanta/internal/shared/random"
	"secretsanta/internal/shared/sanitize"
)

// Noms des champs du formulaire.
const (
	FieldFirstName = "prenom"
	FieldName      = "nom"
	FieldEmail     = "email"
	// FieldHoneypot est le champ piège, invisible pour une personne.
	FieldHoneypot = "site_web"
	// FieldConfirmShared vaut "1" quand la personne confirme partager
	// l'adresse avec un proche.
	FieldConfirmShared = "adresse_partagee"
)

// Bornes des saisies : celles de F3.
const (
	FirstNameMaxLen = participants.FirstNameMaxLen
	NameMaxLen      = participants.NameMaxLen
	EmailMaxLen     = participants.EmailMaxLen
)

// Submission est la saisie du formulaire.
type Submission struct {
	FirstName string
	Name      string
	Email     string
	Honeypot  string
	// ConfirmShared : « Cette adresse est partagée avec un proche ».
	ConfirmShared bool
	// IP est l'adresse du client (plafonds de F11.3) ; Lang sa langue.
	IP   string
	Lang string
}

// Outcome est l'issue d'une soumission.
type Outcome string

const (
	// OutcomeCodeSent : inscription en attente créée, code envoyé.
	OutcomeCodeSent Outcome = "code_envoye"
	// OutcomeInvalid : champs à corriger (voir Fields).
	OutcomeInvalid Outcome = "invalide"
	// OutcomeNameTaken : « ce nom est déjà inscrit », sans dire qui.
	OutcomeNameTaken Outcome = "nom_pris"
	// OutcomeConfirmShared : l'adresse est déjà utilisée dans le groupe ;
	// rien n'est envoyé sans confirmation.
	OutcomeConfirmShared Outcome = "adresse_partagee"
	// OutcomeClosed : inscriptions closes (voir Closed).
	OutcomeClosed Outcome = "close"
)

// Problem est un message (code de catalogue et texte français).
type Problem struct {
	Code    string
	Message string
}

// OfferKind désigne une suite proposée après un nom déjà inscrit.
type OfferKind string

const (
	// OfferMagicLink : « recevoir mon lien » (lien magique de F2).
	OfferMagicLink OfferKind = "lien_magique"
	// OfferPrecision : ajouter une précision au nom (par exemple Camille M.).
	OfferPrecision OfferKind = "precision"
)

// Offer est une suite proposée.
type Offer struct {
	Kind    OfferKind
	Code    string
	Message string
	// Path est la page cible (vide si l'action reste sur le formulaire).
	Path string
}

// Values sont les saisies nettoyées, pour remplir à nouveau le formulaire.
type Values struct {
	FirstName string
	Name      string
	Email     string
}

// Result est la réponse à une soumission. Il ne contient jamais le nom ni
// l'adresse d'un participant ou d'une autre inscription.
type Result struct {
	Outcome Outcome
	Closed  ClosedReason
	Problem Problem
	// Fields : problèmes par champ (FieldFirstName, FieldName, FieldEmail).
	Fields map[string]Problem
	Offers []Offer
	// Ref : référence de l'inscription pour l'étape du code
	// (OutcomeCodeSent).
	Ref    string
	Values Values
}

// Messages et codes du formulaire.
const (
	msgCodeSent      = "Un code de vérification vient de partir à votre adresse."
	msgInvalid       = "Vérifiez les champs signalés."
	msgNameTaken     = "Ce nom est déjà inscrit."
	msgOfferLink     = "Recevoir mon lien"
	msgOfferPrecise  = "Ajoutez une précision à votre nom, par exemple Camille M."
	msgSharedConfirm = "Cette adresse est déjà utilisée dans le groupe. Cochez « Cette adresse est partagée avec un proche » pour recevoir le code."
	msgFirstRequired = "Le prénom est obligatoire."
	msgFirstTooLong  = "Le prénom ne doit pas dépasser 80 caractères."
	msgNameRequired  = "Le nom est obligatoire."
	msgNameTooLong   = "Le nom ne doit pas dépasser 80 caractères."
	msgEmailRequired = "L'adresse mail est obligatoire."
	msgEmailTooLong  = "L'adresse mail ne doit pas dépasser 254 caractères."
	msgEmailInvalid  = "L'adresse mail n'est pas valide."

	codeCodeSent      = "registration.code_sent"
	codeInvalid       = "registration.invalid"
	codeNameTaken     = "registration.name_taken"
	codeOfferLink     = "registration.offer_magic_link"
	codeOfferPrecise  = "registration.offer_precision"
	codeSharedConfirm = "registration.shared_address_confirm"
	codeFirstRequired = "registration.first_name_required"
	codeFirstTooLong  = "registration.first_name_too_long"
	codeNameRequired  = "registration.name_required"
	codeNameTooLong   = "registration.name_too_long"
	codeEmailRequired = "registration.email_required"
	codeEmailTooLong  = "registration.email_too_long"
	codeEmailInvalid  = "registration.email_invalid"
)

// cleanField nettoie une saisie d'une ligne (sanitize de F1) et borne sa
// longueur ; le contrôle de longueur porte sur la valeur nettoyée.
func cleanField(raw string, max int, required, tooLong Problem) (string, *Problem) {
	v, err := sanitize.Line(raw, max, "champ")
	if err != nil {
		return "", &tooLong
	}
	if v == "" {
		return "", &required
	}
	return v, nil
}

func validEmail(s string) bool {
	a, err := mail.ParseAddress(s)
	if err != nil || a.Address != s || a.Name != "" {
		return false
	}
	at := strings.LastIndexByte(s, '@')
	if at <= 0 || at == len(s)-1 {
		return false
	}
	dom := s[at+1:]
	return strings.Contains(dom, ".") && !strings.HasPrefix(dom, ".") && !strings.HasSuffix(dom, ".")
}

func validate(sub Submission) (Values, map[string]Problem) {
	var v Values
	fields := map[string]Problem{}
	var p *Problem
	if v.FirstName, p = cleanField(sub.FirstName, FirstNameMaxLen,
		Problem{codeFirstRequired, msgFirstRequired}, Problem{codeFirstTooLong, msgFirstTooLong}); p != nil {
		fields[FieldFirstName] = *p
	}
	if v.Name, p = cleanField(sub.Name, NameMaxLen,
		Problem{codeNameRequired, msgNameRequired}, Problem{codeNameTooLong, msgNameTooLong}); p != nil {
		fields[FieldName] = *p
	}
	if v.Email, p = cleanField(sub.Email, EmailMaxLen,
		Problem{codeEmailRequired, msgEmailRequired}, Problem{codeEmailTooLong, msgEmailTooLong}); p != nil {
		fields[FieldEmail] = *p
	} else if !validEmail(v.Email) {
		fields[FieldEmail] = Problem{codeEmailInvalid, msgEmailInvalid}
	}
	return v, fields
}

// codeSent est la réponse d'une inscription acceptée ; le champ piège rempli
// reçoit exactement la même forme, avec une référence aléatoire jamais
// enregistrée.
func codeSent(ref string) *Result {
	return &Result{Outcome: OutcomeCodeSent, Problem: Problem{codeCodeSent, msgCodeSent}, Ref: ref}
}

// Submit traite le formulaire d'inscription de /<slug>/rejoindre/<jeton>.
// ErrNeutral si le slug, le mode ou le jeton ne conviennent pas. Ordre :
// jeton, champ piège, fermeture, validations, unicité du nom (participants
// par participants.NameExists et inscriptions en attente), adresse partagée,
// plafonds (Throttle), création de l'inscription puis émission du code.
func (s *Service) Submit(ctx context.Context, slug, token string, sub Submission) (*Result, error) {
	g, inv, err := s.resolve(ctx, slug, token)
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(sub.Honeypot) != "" {
		// Refus silencieux : même réponse qu'une inscription acceptée, rien
		// n'est enregistré ni envoyé.
		ref, err := random.Token()
		if err != nil {
			return nil, errs.Internal(err)
		}
		s.cfg.Logger.Debug("inscription : champ piège rempli", "groupe", g.ID)
		return codeSent(ref), nil
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	now := s.cfg.Clock.Now().UTC()
	closed, err := s.closedReason(ctx, g.ID, inv, now)
	if err != nil {
		return nil, err
	}
	if closed != "" {
		code, msg := closed.Message()
		return &Result{Outcome: OutcomeClosed, Closed: closed, Problem: Problem{code, msg}}, nil
	}

	v, fields := validate(sub)
	if len(fields) > 0 {
		return &Result{Outcome: OutcomeInvalid, Problem: Problem{codeInvalid, msgInvalid}, Fields: fields, Values: v}, nil
	}

	key := participants.NormalizeName(v.FirstName, v.Name)
	taken, err := s.cfg.Directory.NameExists(ctx, g.ID, v.FirstName, v.Name)
	if err != nil {
		return nil, err
	}
	if !taken {
		if taken, err = s.cfg.Store.PendingNameExists(ctx, g.ID, key, now); err != nil {
			return nil, err
		}
	}
	if taken {
		return &Result{
			Outcome: OutcomeNameTaken,
			Problem: Problem{codeNameTaken, msgNameTaken},
			Fields:  map[string]Problem{FieldName: {codeNameTaken, msgNameTaken}},
			Offers: []Offer{
				{Kind: OfferMagicLink, Code: codeOfferLink, Message: msgOfferLink, Path: "/" + g.Slug + "/connexion"},
				{Kind: OfferPrecision, Code: codeOfferPrecise, Message: msgOfferPrecise},
			},
			Values: v,
		}, nil
	}

	ekey := emailKey(v.Email)
	known, err := s.cfg.Directory.EmailKnown(ctx, g.ID, v.Email)
	if err != nil {
		return nil, err
	}
	if !known {
		if known, err = s.cfg.Store.PendingEmailExists(ctx, g.ID, ekey, now); err != nil {
			return nil, err
		}
	}
	if known && !sub.ConfirmShared {
		return &Result{Outcome: OutcomeConfirmShared, Problem: Problem{codeSharedConfirm, msgSharedConfirm}, Values: v}, nil
	}

	if s.cfg.Throttle != nil {
		if err := s.cfg.Throttle.AllowRegistration(ctx, g.ID, sub.IP, ekey); err != nil {
			return nil, err
		}
	}

	ref, err := random.Token()
	if err != nil {
		return nil, errs.Internal(err)
	}
	lang := sub.Lang
	if utf8.RuneCountInString(lang) > participants.LocaleMaxLen || (lang != "" && !participants.ValidLocale(lang)) {
		lang = ""
	}
	p, err := s.cfg.Store.AddPending(ctx, Pending{
		GroupID: g.ID, Ref: ref, FirstName: v.FirstName, Name: v.Name, NameKey: key,
		Email: v.Email, EmailKey: ekey, SharedAddress: known, Lang: lang,
		CreatedAt: now, ExpiresAt: now.Add(PendingTTL),
	})
	if err != nil {
		return nil, err
	}
	if err := s.cfg.Codes.IssueCode(ctx, p); err != nil {
		if derr := s.cfg.Store.DeletePending(ctx, g.ID, p.ID); derr != nil {
			s.cfg.Logger.Error("inscription : annulation impossible", "groupe", g.ID, "erreur", derr)
		}
		return nil, err
	}
	return codeSent(ref), nil
}
