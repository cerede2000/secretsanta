# Design (FROZEN)

- Source: Artefact Claude Design https://claude.ai/artifact/TUXyQa6JejQnS6gncNp2XY, instantané reconstruit à partir des fichiers publiés (la lecture de la page ne renvoyait que la coquille de l'éditeur)
- Approved: 2026-11-15
- Snapshot: docs/waves/design/snapshot.html

| Screen id | Feature |
|---|---|
| screen-accueil-groupe | F2 |
| screen-admin-ajuster | F10 |
| screen-admin-connexion | F2 |
| screen-admin-delivrabilite | F6 |
| screen-admin-fiche | F10 |
| screen-admin-inscriptions | F11 |
| screen-admin-menu | F10 |
| screen-admin-participants | F10 |
| screen-admin-reglages | F10 |
| screen-admin-tableau | F10 |
| screen-admin-tableau-bureau | F10 |
| screen-admin-tirage | F10 |
| screen-attente-revelation | F4 |
| screen-boite-lettres | F5 |
| screen-connexion | F2 |
| screen-creation-groupe | F2 |
| screen-desinscription | F6 |
| screen-inscription | F11 |
| screen-inscription-code | F11 |
| screen-inscription-reussie | F11 |
| screen-lettre | F5 |
| screen-lettre-recue | F5 |
| screen-ma-cible | F4 |
| screen-reception-seule | F4 |
| screen-revelation | F9 |
| screen-revelation-b | F9 |
| screen-revelation-c | F9 |
| screen-suggestions | F8 |
| tokens | F9 (thème ; valable pour toutes) |

Fichiers sources exacts : docs/waves/design/source/ (un .dc.html par écran, Main.dc.html = tokens, canvas.json).

## Tokens
- Palette : rouge #c41e3a (action principale), vert sapin #165b33 (admin, secondaire), or #ffd700 (lueurs, bouton sur nuit, jamais en texte sur fond clair), bleu nuit #0f1b3d vers #1c2f6b (fond festif), givre menthe #eaf4ee (fond clair, cartes), contours #c5ddd0, encre #1f2d26 (texte), texte secondaire #4a5a50.
- Polices : Mountains of Christmas 700 (titres) et Nunito 400/600/700/800 (texte), embarquées en local, jamais chargées depuis Internet.
- Formes : rayons 8, 14, 20 et pilule ; boutons de 48 px de haut, cibles tactiles de 44 px minimum ; mobile d'abord (360 et 390 px), animations coupées par « réduire les animations ».
- Un statut n'est jamais porté par la couleur seule : icône et mot.

## Décisions de design à connaître
- La révélation tire au hasard un des 3 styles (cadeau, sceau de cire, carte à gratter), sans répéter le précédent ; avec deux cadeaux, un seul est découvert à la fois, chacun avec son propre style tiré au hasard.
- Budget facultatif : sans budget, la ligne « Budget » disparaît (cible, attente, révélation, lettre) ; défini plus tard, un bandeau « Budget mis à jour » apparaît sur la carte de la cible.
- Rôle dans le tirage unique par personne : Normal (défaut), Réception seule, Offre des cadeaux en plus, Un seul cadeau. Chaque personne reçoit exactement un cadeau ; plafond équitable de 2, confirmation au-delà ; rien ne révèle qui offre à qui.
- Inscription par lien : la section « Un coup de pouce ? (facultatif) » n'apparaît que si l'organisateur a activé l'option ; rien n'est présélectionné ; les deux choix (ne peut pas offrir / accepte d'offrir en plus) sont exclusifs et annulables.
- Admin : barre basse à 5 entrées (Tableau, Participants, Tirage, Inscriptions, Plus) sur mobile, barre latérale en 1024 px.
- Le QR code de la maquette est un motif factice.
- Le diagnostic de délivrabilité contrôle SPF, DKIM, DMARC, MX, alignement, TLS SMTP et HTTPS ; il ne voit pas la réputation IP, les listes noires ni le DNS inverse.

FROZEN: changes after approval require replanning.
