package overlay

import "unicode"

const (
	fuzzyConsecutiveBonus  = 15
	fuzzyBoundaryBonus     = 10
	fuzzyEarlyMatchWeight  = 1
	fuzzyShortTargetWeight = 1
)

func isBoundaryRune(r rune) bool {
	switch r {
	case '-', '_', '/', '.', ' ':
		return true
	}
	return false
}

// fuzzyScore scores target against pattern as a subsequence match, fzf-like.
// All runes of pattern must appear in target, in order, case-insensitively.
func fuzzyScore(pattern, target string) (score int, matched bool) {
	patternRunes := []rune(pattern)
	if len(patternRunes) == 0 {
		return 0, true
	}
	targetRunes := []rune(target)

	pi := 0
	firstMatch := -1
	lastMatchIdx := -1
	consecutive := 0

	for ti := 0; ti < len(targetRunes) && pi < len(patternRunes); ti++ {
		tr := targetRunes[ti]
		pr := patternRunes[pi]
		if unicode.ToLower(tr) != unicode.ToLower(pr) {
			continue
		}

		if firstMatch == -1 {
			firstMatch = ti
		}

		if lastMatchIdx == ti-1 {
			consecutive++
			score += fuzzyConsecutiveBonus * consecutive
		} else {
			consecutive = 0
		}

		boundary := ti == 0
		if ti > 0 {
			prev := targetRunes[ti-1]
			if isBoundaryRune(prev) {
				boundary = true
			} else if unicode.IsLower(prev) && unicode.IsUpper(tr) {
				boundary = true
			}
		}
		if boundary {
			score += fuzzyBoundaryBonus
		}

		lastMatchIdx = ti
		pi++
	}

	if pi < len(patternRunes) {
		return 0, false
	}

	score += fuzzyEarlyMatchWeight * max(0, 100-firstMatch)
	score += fuzzyShortTargetWeight * max(0, 200-len(targetRunes))

	return score, true
}
