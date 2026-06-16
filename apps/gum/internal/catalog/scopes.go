package catalog

import "sort"

// ScopesForOp returns the OAuth scopes declared on opID's default variant —
// the exact set the dispatch kernel requests when that op runs. Returns nil
// when the op is unknown, has no default variant, or that variant declares no
// scopes. The result is a copy; mutating it does not affect the catalog.
func (c *Catalog) ScopesForOp(opID string) []string {
	for i := range c.Ops {
		op := &c.Ops[i]
		if op.OpID != opID {
			continue
		}
		for j := range op.Variants {
			v := &op.Variants[j]
			if v.VariantID == op.DefaultVariantID {
				if len(v.Scopes) == 0 {
					return nil
				}
				return append([]string(nil), v.Scopes...)
			}
		}
		return nil
	}
	return nil
}

// ScopesForServices returns the sorted, de-duplicated union of OAuth scopes
// across every op whose Service is in the given set. `gum login` uses this for
// its core-services default and `--service` opt-in, so a BYO consent screen
// isn't asked to grant scopes for APIs the project hasn't enabled.
func (c *Catalog) ScopesForServices(services []string) []string {
	want := make(map[string]struct{}, len(services))
	for _, s := range services {
		want[s] = struct{}{}
	}
	seen := map[string]struct{}{}
	for i := range c.Ops {
		if _, ok := want[c.Ops[i].Service]; !ok {
			continue
		}
		for j := range c.Ops[i].Variants {
			for _, s := range c.Ops[i].Variants[j].Scopes {
				if s != "" {
					seen[s] = struct{}{}
				}
			}
		}
	}
	if len(seen) == 0 {
		return nil
	}
	out := make([]string, 0, len(seen))
	for s := range seen {
		out = append(out, s)
	}
	sort.Strings(out)
	return out
}

// AllScopes returns the sorted, de-duplicated union of every OAuth scope across
// all ops and variants in the catalog. `gum login --all` uses this to request
// the full set in a single consent screen. Returns nil when no op declares a scope.
func (c *Catalog) AllScopes() []string {
	seen := map[string]struct{}{}
	for i := range c.Ops {
		for j := range c.Ops[i].Variants {
			for _, s := range c.Ops[i].Variants[j].Scopes {
				if s == "" {
					continue
				}
				seen[s] = struct{}{}
			}
		}
	}
	if len(seen) == 0 {
		return nil
	}
	out := make([]string, 0, len(seen))
	for s := range seen {
		out = append(out, s)
	}
	sort.Strings(out)
	return out
}
