package feature

import (
	"sort"
	"strings"

	"github.com/maisam9060/platform-api/internal/config"
	"github.com/maisam9060/platform-api/internal/hash"
)

// ComputeFeatureHash calculates hash of feature
func ComputeFeatureHash(
	f *config.Feature,
	depHashes map[string]string,
) (string, error) {

	var parts []string

	// Hash command
	parts = append(parts, hash.HashString(f.Command))

	// Hash inputs
	for _, input := range f.Inputs {
		h, err := hash.HashDir(input)
		if err != nil {
			return "", err
		}
		parts = append(parts, h)
	}

	// Hash dependencies (sorted for determinism)
	var deps []string
	for dep, h := range depHashes {
		deps = append(deps, dep+h)
	}
	sort.Strings(deps)

	parts = append(parts, deps...)

	final := strings.Join(parts, "|")
	return hash.HashString(final), nil
}
