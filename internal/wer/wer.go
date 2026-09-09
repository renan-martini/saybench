// Package wer computes word error rate between a reference transcript and a
// hypothesis, with the substitution/deletion/insertion breakdown that makes a
// score debuggable.
package wer

import (
	"strings"
	"unicode"
)

// Counts holds the edit operations from aligning a hypothesis against a
// reference, plus the reference length that normalizes them.
type Counts struct {
	Sub      int `json:"sub"`
	Del      int `json:"del"`
	Ins      int `json:"ins"`
	RefWords int `json:"ref_words"`
}

// WER returns (S+D+I)/N. A zero-length reference with a non-empty hypothesis
// is all insertions and returns 1; two empty strings return 0.
func (c Counts) WER() float64 {
	if c.RefWords == 0 {
		if c.Ins > 0 {
			return 1
		}
		return 0
	}
	return float64(c.Sub+c.Del+c.Ins) / float64(c.RefWords)
}

// Normalize lowercases, strips punctuation (keeping intra-word apostrophes),
// and splits into words. Both sides of a comparison go through it, so casing
// and punctuation choices by a vendor never count as errors.
func Normalize(s string) []string {
	var b strings.Builder
	runes := []rune(strings.ToLower(s))
	for i, r := range runes {
		switch {
		case unicode.IsLetter(r) || unicode.IsDigit(r):
			b.WriteRune(r)
		case r == '\'' && i > 0 && i < len(runes)-1 &&
			unicode.IsLetter(runes[i-1]) && unicode.IsLetter(runes[i+1]):
			b.WriteRune(r) // it's, o'clock
		default:
			b.WriteRune(' ')
		}
	}
	return strings.Fields(b.String())
}

// Compute aligns hypothesis words against reference words with a standard
// Levenshtein DP and backtracks to attribute each edit.
func Compute(reference, hypothesis string) Counts {
	ref := Normalize(reference)
	hyp := Normalize(hypothesis)

	n, m := len(ref), len(hyp)
	// dp[i][j] = min edits aligning ref[:i] with hyp[:j].
	dp := make([][]int, n+1)
	for i := range dp {
		dp[i] = make([]int, m+1)
		dp[i][0] = i
	}
	for j := 0; j <= m; j++ {
		dp[0][j] = j
	}
	for i := 1; i <= n; i++ {
		for j := 1; j <= m; j++ {
			if ref[i-1] == hyp[j-1] {
				dp[i][j] = dp[i-1][j-1]
				continue
			}
			dp[i][j] = 1 + min(dp[i-1][j-1], dp[i-1][j], dp[i][j-1])
		}
	}

	c := Counts{RefWords: n}
	for i, j := n, m; i > 0 || j > 0; {
		switch {
		case i > 0 && j > 0 && ref[i-1] == hyp[j-1] && dp[i][j] == dp[i-1][j-1]:
			i, j = i-1, j-1
		case i > 0 && j > 0 && dp[i][j] == dp[i-1][j-1]+1:
			c.Sub++
			i, j = i-1, j-1
		case i > 0 && dp[i][j] == dp[i-1][j]+1:
			c.Del++
			i--
		default:
			c.Ins++
			j--
		}
	}
	return c
}

// KeytermHits checks which terms appear in the hypothesis as exact word
// sequences (after the same normalization as WER). It answers the question a
// corpus-level WER hides: did the transcript get the words that matter — the
// names, product terms, and codes — regardless of how the filler fared?
func KeytermHits(hypothesis string, terms []string) (hit, missed []string) {
	hyp := Normalize(hypothesis)
	for _, term := range terms {
		want := Normalize(term)
		if len(want) == 0 {
			continue
		}
		if containsSeq(hyp, want) {
			hit = append(hit, term)
		} else {
			missed = append(missed, term)
		}
	}
	return hit, missed
}

func containsSeq(words, seq []string) bool {
	if len(seq) > len(words) {
		return false
	}
outer:
	for i := 0; i+len(seq) <= len(words); i++ {
		for j := range seq {
			if words[i+j] != seq[j] {
				continue outer
			}
		}
		return true
	}
	return false
}

// WordSurvival reports how many of the final transcript's distinct
// normalized words appeared in at least one interim — the "were the interims
// telling the truth?" metric for streaming STT. Distinct-word counting by
// design: vendors stream with different semantics (replacement hypotheses vs
// append-only deltas), and per-occurrence accounting would need per-vendor
// rules. A repeated final word counts once.
func WordSurvival(final string, interims []string) (hit, total int) {
	seen := map[string]bool{}
	for _, in := range interims {
		for _, w := range Normalize(in) {
			seen[w] = true
		}
	}
	counted := map[string]bool{}
	for _, w := range Normalize(final) {
		if counted[w] {
			continue
		}
		counted[w] = true
		total++
		if seen[w] {
			hit++
		}
	}
	return hit, total
}
