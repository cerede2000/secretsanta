package maildiag

// NotVerifiableItems liste ce que l'application ne contrôle pas : la
// réputation de l'adresse IP, les listes noires et le DNS inverse (PTR).
func NotVerifiableItems() []NotVerifiable {
	return []NotVerifiable{
		{ID: "ip_reputation", Guide: GuideAnchor},
		{ID: "blocklists", Guide: GuideAnchor},
		{ID: "reverse_dns", Guide: GuideAnchor},
	}
}
