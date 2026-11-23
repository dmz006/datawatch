package packs

// InstalledPack is the record Install returns for a pack copied into a
// destination root. Its Dir is the copied folder, destRoot/<name>/<version>/.
type InstalledPack struct {
	// Name and Version are the pack.yaml identity the folder was installed under.
	Name    string
	Version string
	// Digest is the content digest LoadPackFolder computed over the source
	// folder. The copy holds the same files, so the digest matches the copy too.
	Digest string
	// SourceDir is the absolute pack folder the install was read from.
	SourceDir string
	// Dir is the absolute folder the pack was copied to.
	Dir string
}
