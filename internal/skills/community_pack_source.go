package skills

// community_pack_source.go identifies first-party pack sources: registries
// the datawatch project itself maintains and seeds as built-in defaults.
// The community registry is the first such source. Packs are never copied
// into the binary or the repository; they are only ever read from the
// registry's git clone, so the flag is purely a provenance marker.

// firstPartyRegistries returns the built-in registry definitions flagged
// FirstParty. Kept as a function so the set is derived from the single
// source of truth (the seeded default vars) rather than a second list.
func firstPartyRegistries() []*Registry {
	candidates := []*Registry{CommunityDefaultRegistry, PAIDefaultRegistry}
	out := make([]*Registry, 0, len(candidates))
	for _, r := range candidates {
		if r != nil && r.FirstParty {
			out = append(out, r)
		}
	}
	return out
}

// IsFirstPartyPackSource reports whether name refers to a first-party pack
// source (a built-in registry flagged FirstParty). An empty name is never
// first-party.
func IsFirstPartyPackSource(name string) bool {
	if name == "" {
		return false
	}
	for _, r := range firstPartyRegistries() {
		if r.Name == name {
			return true
		}
	}
	return false
}
