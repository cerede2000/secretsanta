# Délivrabilité des courriels

Pour que les courriels arrivent en boîte de réception, le domaine d'expédition doit prouver qu'il est autorisé à envoyer. Les exemples ci-dessous sont à adapter : remplacez `example.org`, l'adresse IP `203.0.113.10` et le sélecteur `noel`.

## SPF

Enregistrement TXT sur `example.org` listant les serveurs autorisés :

```
example.org.  IN TXT  "v=spf1 ip4:203.0.113.10 include:_spf.relais.example ~all"
```

## DKIM

Enregistrement TXT sur `noel._domainkey.example.org` contenant la clé publique :

```
noel._domainkey.example.org.  IN TXT  "v=DKIM1; k=rsa; p=MIIBIjANBgkq...clé publique..."
```

## DMARC

Enregistrement TXT sur `_dmarc.example.org`. Commencez en surveillance, puis durcissez :

```
_dmarc.example.org.  IN TXT  "v=DMARC1; p=none; rua=mailto:dmarc@example.org; adkim=s; aspf=s"
```

Passez à `p=quarantine`, puis `p=reject`, quand les rapports sont propres.

## DNS inverse (PTR)

L'adresse IP d'envoi doit avoir un enregistrement PTR vers un nom qui revient vers la même IP. Il se demande à l'hébergeur de l'adresse IP (vous ne pouvez pas le publier vous-même). Réglez `MAIL_EHLO_HOSTNAME` sur ce même nom, par exemple `mail.example.org`.

## Échauffement progressif de l'adresse IP

Une adresse IP neuve n'a pas de réputation. Commencez par quelques dizaines de messages par jour vers des destinataires qui répondent, doublez peu à peu sur deux à quatre semaines, et limitez le débit avec `MAIL_MAX_PER_MINUTE`. Évitez les gros envois d'un coup.

## Vérification des listes noires

Vérifiez régulièrement l'adresse IP et le domaine sur des services de consultation de listes noires (Spamhaus, MXToolbox, Google Postmaster Tools). Si l'adresse figure sur une liste, suivez la procédure de retrait du fournisseur de la liste.

## Signature DKIM par l'application

Quand votre relais ne signe pas les messages, l'application peut le faire : définissez ensemble `MAIL_DKIM_DOMAIN`, `MAIL_DKIM_SELECTOR`, `MAIL_DKIM_KEY_FILE` (fichier de clé privée monté en volume ou secret, jamais en clair dans une variable) et `MAIL_DKIM_ALGORITHM`. Publiez la clé publique correspondante comme dans la section DKIM. Si le relais signe déjà, ne doublez pas la signature.

## Ce que l'application fait et ne fait pas

Elle fait : des messages courts, en texte et HTML équivalents, avec liens https uniquement, sans pixel espion ni script ; les en-têtes List-Unsubscribe et désinscription en un clic pour les messages non essentiels ; la signature DKIM facultative ; la limitation de débit ; l'arrêt des envois vers une adresse qui rebondit durablement (5xx).

Elle ne fait pas : publier vos enregistrements SPF, DKIM, DMARC ou PTR, ni choisir la réputation de votre adresse IP. Elle ne peut pas garantir la livraison : la décision finale appartient au fournisseur du destinataire. Le score de `scripts/spamcheck.sh` mesure le contenu des messages, pas la réputation ni les listes noires.
