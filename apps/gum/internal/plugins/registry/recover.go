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
	files, err := r.Load()
	if err != nil {
		return Generation{}, err
	}
	cgPresent := fileExists(CatalogPath(r.profileDir))
	lkPresent := fileExists(LockPath(r.profileDir))
	stPresent := fileExists(StatePath(r.profileDir))

	// Clean slate: no files yet → "generation 0" is authoritative by
	// vacuous truth so the host can boot without warnings.
	if !cgPresent && !lkPresent && !stPresent {
		return Generation{Generation: 0, TxID: "", Ok: true}, nil
	}
	// Partial presence is incomplete by definition.
	if !cgPresent || !lkPresent || !stPresent {
		return Generation{Ok: false}, nil
	}
	if files.Lock.InstallGeneration != files.State.InstallGeneration ||
		files.Lock.InstallTxID != files.State.InstallTxID {
		return Generation{Ok: false}, nil
	}
	// plugin-catalog.json does not carry install_generation in its v1 shape
	// (spec §8.7 line 1707-1723), so we only verify it is present and has the
	// supported schema version — which Load already guaranteed by reaching
	// here. The "shared generation" check is therefore Lock == State.
	return Generation{Generation: files.Lock.InstallGeneration, TxID: files.Lock.InstallTxID, Ok: true}, nil
}

func fileExists(path string) bool {
	_, ok, err := readIfExists(path)
	if err != nil {
		return false
	}
	return ok
}
