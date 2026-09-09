package wer

import "testing"

func TestCompute(t *testing.T) {
	tests := []struct {
		name     string
		ref, hyp string
		want     Counts
		wantWER  float64
	}{
		{"exact", "the quick brown fox", "the quick brown fox", Counts{RefWords: 4}, 0},
		{"case and punctuation ignored", "Hello, World!", "hello world", Counts{RefWords: 2}, 0},
		{"one substitution", "call me tomorrow", "call me today", Counts{Sub: 1, RefWords: 3}, 1.0 / 3},
		{"one deletion", "the big red ball", "the red ball", Counts{Del: 1, RefWords: 4}, 0.25},
		{"one insertion", "send the report", "send me the report", Counts{Ins: 1, RefWords: 3}, 1.0 / 3},
		{"empty hypothesis", "three words here", "", Counts{Del: 3, RefWords: 3}, 1},
		{"empty reference nonempty hyp", "", "noise", Counts{Ins: 1}, 1},
		{"both empty", "", "", Counts{}, 0},
		{"apostrophe kept", "it's o'clock", "it's o'clock", Counts{RefWords: 2}, 0},
		{"all wrong", "alpha beta", "gamma delta", Counts{Sub: 2, RefWords: 2}, 1},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := Compute(tt.ref, tt.hyp)
			if got != tt.want {
				t.Fatalf("Compute() = %+v, want %+v", got, tt.want)
			}
			if w := got.WER(); w != tt.wantWER {
				t.Fatalf("WER() = %v, want %v", w, tt.wantWER)
			}
		})
	}
}

func TestNormalize(t *testing.T) {
	got := Normalize("  The  QUICK-brown fox, jumped!  ")
	want := []string{"the", "quick", "brown", "fox", "jumped"}
	if len(got) != len(want) {
		t.Fatalf("Normalize() = %v, want %v", got, want)
	}
	for i := range got {
		if got[i] != want[i] {
			t.Fatalf("Normalize()[%d] = %q, want %q", i, got[i], want[i])
		}
	}
}

func TestKeytermHits(t *testing.T) {
	hyp := "hi this is marcus aurelio calling about the mri appointment"
	hit, missed := KeytermHits(hyp, []string{"Marcus Aurelio", "MRI", "Beatriz Nakamura", ""})
	if len(hit) != 2 || hit[0] != "Marcus Aurelio" || hit[1] != "MRI" {
		t.Fatalf("hit = %v", hit)
	}
	if len(missed) != 1 || missed[0] != "Beatriz Nakamura" {
		t.Fatalf("missed = %v", missed)
	}
	// Partial word must not match: "aurelios" is not "aurelio".
	hit, _ = KeytermHits("marcus aurelios", []string{"Marcus Aurelio"})
	if len(hit) != 0 {
		t.Fatalf("partial word matched: %v", hit)
	}
}

func TestWordSurvival(t *testing.T) {
	tests := []struct {
		name     string
		final    string
		interims []string
		hit, tot int
	}{
		{"all seen", "pay two hundred", []string{"pay two", "pay two hundred"}, 3, 3},
		{"late word never previewed", "pay two hundred dollars", []string{"pay two hundred"}, 3, 4},
		{"revised interim words don't matter", "pay two hundred", []string{"play tooth"}, 0, 3},
		{"no interims", "pay two", nil, 0, 2},
		{"empty final", "", []string{"noise"}, 0, 0},
		{"repeats count once", "seven seven seven", []string{"seven"}, 1, 1},
		{"normalization applies", "Pay $200!", []string{"pay 200"}, 2, 2},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			hit, tot := WordSurvival(tt.final, tt.interims)
			if hit != tt.hit || tot != tt.tot {
				t.Fatalf("WordSurvival() = %d/%d, want %d/%d", hit, tot, tt.hit, tt.tot)
			}
		})
	}
}
