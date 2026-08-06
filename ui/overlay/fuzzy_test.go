package overlay

import "testing"

func TestFuzzyScoreEmptyPatternMatchesEverything(t *testing.T) {
	score, matched := fuzzyScore("", "anything")
	if !matched || score != 0 {
		t.Fatalf("got score=%d matched=%v, want score=0 matched=true", score, matched)
	}
}

func TestFuzzyScoreSubsequenceMatches(t *testing.T) {
	_, matched := fuzzyScore("rmt-agents", "remote-agents")
	if !matched {
		t.Fatalf("expected rmt-agents to match remote-agents as a subsequence")
	}
}

func TestFuzzyScoreNonSubsequenceRejects(t *testing.T) {
	_, matched := fuzzyScore("xyz", "remote-agents")
	if matched {
		t.Fatalf("expected xyz to not match remote-agents")
	}
}

func TestFuzzyScoreOutOfOrderRejects(t *testing.T) {
	_, matched := fuzzyScore("tsr", "rst")
	if matched {
		t.Fatalf("expected out-of-order pattern to not match")
	}
}

func TestFuzzyScoreCaseInsensitive(t *testing.T) {
	scoreLower, matchedLower := fuzzyScore("remote", "remote-agents")
	scoreUpper, matchedUpper := fuzzyScore("REMOTE", "remote-agents")
	if !matchedLower || !matchedUpper {
		t.Fatalf("expected both cases to match")
	}
	if scoreLower != scoreUpper {
		t.Fatalf("expected case-insensitive scores to be equal, got %d vs %d", scoreLower, scoreUpper)
	}
}

func TestFuzzyScoreBoundaryOutranksScattered(t *testing.T) {
	// "ra" matches remote-agents at a word boundary (the "a" in "agents"),
	// but in a target where r and a are scattered mid-word with no boundary
	// help, the score should be lower.
	boundaryScore, matched1 := fuzzyScore("ra", "remote-agents")
	scatteredScore, matched2 := fuzzyScore("ra", "xrxxxxxaxx")
	if !matched1 || !matched2 {
		t.Fatalf("expected both to match")
	}
	if boundaryScore <= scatteredScore {
		t.Fatalf("expected boundary match to outrank scattered match: %d vs %d", boundaryScore, scatteredScore)
	}
}

func TestFuzzyScoreConsecutiveOutranksSpread(t *testing.T) {
	consecutive, matched1 := fuzzyScore("re", "remote")
	spread, matched2 := fuzzyScore("re", "roeote")
	if !matched1 || !matched2 {
		t.Fatalf("expected both to match")
	}
	if consecutive <= spread {
		t.Fatalf("expected consecutive match to outrank spread match: %d vs %d", consecutive, spread)
	}
}

func TestFuzzyScorePrefixOutranksLateMatch(t *testing.T) {
	prefix, matched1 := fuzzyScore("re", "remote-agents")
	late, matched2 := fuzzyScore("re", "some-other-remote")
	if !matched1 || !matched2 {
		t.Fatalf("expected both to match")
	}
	if prefix <= late {
		t.Fatalf("expected earlier match to outrank later match: %d vs %d", prefix, late)
	}
}

func TestFuzzyScoreShorterTargetOutranksLonger(t *testing.T) {
	short, matched1 := fuzzyScore("ab", "ab")
	long, matched2 := fuzzyScore("ab", "ab-with-a-lot-of-extra-noise-around-it")
	if !matched1 || !matched2 {
		t.Fatalf("expected both to match")
	}
	if short <= long {
		t.Fatalf("expected shorter target to outrank longer target: %d vs %d", short, long)
	}
}

func TestFuzzyScoreNonASCIIDoesNotPanic(t *testing.T) {
	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("fuzzyScore panicked on non-ASCII input: %v", r)
		}
	}()
	score, matched := fuzzyScore("café", "café-agents")
	if !matched || score <= 0 {
		t.Fatalf("expected café to match café-agents, got score=%d matched=%v", score, matched)
	}

	// Non-ASCII runes must be indexed correctly, not corrupted by byte offsets.
	score2, matched2 := fuzzyScore("日本", "日本語-repo")
	if !matched2 || score2 <= 0 {
		t.Fatalf("expected 日本 to match 日本語-repo, got score=%d matched=%v", score2, matched2)
	}
}

func TestFuzzyScoreCamelCaseBoundary(t *testing.T) {
	camel, matched1 := fuzzyScore("A", "myAwesomeRepo")
	noBoundary, matched2 := fuzzyScore("w", "myAwesomeRepo")
	if !matched1 || !matched2 {
		t.Fatalf("expected both to match")
	}
	if camel <= noBoundary {
		t.Fatalf("expected camelCase boundary match to outrank non-boundary match: %d vs %d", camel, noBoundary)
	}
}
