package registry

// Generation describes the install generation currently authoritative on disk.
// Ok is true iff all three files exist AND share the same (Generation, TxID).
// Spec §8.7 step 5: "A generation is authoritative only when all three final
// files exist and carry the same install_generation and install_txid."
type Generation struct {
	Generation int
	TxID       string
	Ok         bool
}

// SelectGeneration is the startup recovery routine. It reads the three files
// from disk and reports whether their install_generation + install_txid agree.
//
//   - If all three are absent (empty profile), returns Generation{Ok:true,
//     Generation:0, TxID:""} so callers treat the registry as a clean slate.
//   - If any one of the three is missing or carries a different (gen, txid)
//     than the others, Ok is false. Callers MUST refuse dispatch from the
//     incomplete generation (spec §8.7 step 5).
//   - Unsupported schema versions surface the catalog package's sentinel
//     errors and Ok is false.
func (r *Registry) SelectGeneration() (Generation, error) {
	gen, _, err := r.SelectGenerationFiles()
	return gen, err
}

// SelectGenerationFiles is SelectGeneration plus the parsed files the
// decision was made from. A caller that needs both (the session-catalog
// merge needs the generation to decide whether to trust the rows, and the
// rows themselves to merge them) reads the profile once instead of twice.
func (r *Registry) SelectGenerationFiles() (Generation, *Files, error) {
	files, err := r.Load()
	if err != nil {
		return Generation{}, nil, err
	}
	cgPresent := fileExists(CatalogPath(r.profileDir))
	lkPresent := fileExists(LockPath(r.profileDir))
	stPresent := fileExists(StatePath(r.profileDir))

	// Clean slate: no files yet → "generation 0" is authoritative by
	// vacuous truth so the host can boot without warnings.
	if !cgPresent && !lkPresent && !stPresent {
		return Generation{Generation: 0, TxID: "", Ok: true}, files, nil
	}
	// Partial presence is incomplete by definition.
	if !cgPresent || !lkPresent || !stPresent {
		return Generation{Ok: false}, files, nil
	}
	if files.Lock.InstallGeneration != files.State.InstallGeneration ||
		files.Lock.InstallTxID != files.State.InstallTxID {
		return Generation{Ok: false}, files, nil
	}
	// A catalog written before gum-t3tl carries no stamp at all. Refusing it
	// would brick the profile: the startup activation write that re-stamps
	// the three files only runs after a complete generation is selected, so
	// there would be no path back. Generations start at 1, so (0, "") is
	// unambiguously "not yet stamped" and the catalog adopts the lock/state
	// generation. The next WriteTransaction stamps it and the exemption ends.
	unstamped := files.Catalog.InstallGeneration == 0 && files.Catalog.InstallTxID == ""
	if !unstamped && (files.Catalog.InstallGeneration != files.Lock.InstallGeneration ||
		files.Catalog.InstallTxID != files.Lock.InstallTxID) {
		return Generation{Ok: false}, files, nil
	}
	return Generation{Generation: files.Lock.InstallGeneration, TxID: files.Lock.InstallTxID, Ok: true}, files, nil
}

func fileExists(path string) bool {
	_, ok, err := readIfExists(path)
	if err != nil {
		return false
	}
	return ok
}
