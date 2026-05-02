package reconcile

import (
	"testing"
	"testing/quick"

	"github.com/wyvernzora/kura/internal/domain/refs"
)

func TestDeriveStepIDIsDeterministic(t *testing.T) {
	episode, _ := refs.NewEpisode(1, 3)
	owner := Owner{Kind: OwnerEpisode, EpisodeRef: episode}
	got1 := DeriveStepID("abcdef123456", owner, StepFileMove, "from.mkv", "to.mkv", "")
	got2 := DeriveStepID("abcdef123456", owner, StepFileMove, "from.mkv", "to.mkv", "")
	if got1 != got2 {
		t.Fatalf("non-deterministic: %q != %q", got1, got2)
	}
}

func TestDeriveStepIDLength(t *testing.T) {
	episode, _ := refs.NewEpisode(1, 3)
	id := DeriveStepID("abcdef123456", Owner{Kind: OwnerEpisode, EpisodeRef: episode}, StepFileMove, "a", "b", "")
	if len(id) != 16 {
		t.Fatalf("len(id) = %d, want 16", len(id))
	}
}

func TestDeriveStepIDDistinctInputsDistinctOutputs(t *testing.T) {
	ep1, _ := refs.NewEpisode(1, 1)
	ep2, _ := refs.NewEpisode(1, 2)
	a := DeriveStepID("token", Owner{Kind: OwnerEpisode, EpisodeRef: ep1}, StepFileMove, "x", "y", "")
	b := DeriveStepID("token", Owner{Kind: OwnerEpisode, EpisodeRef: ep2}, StepFileMove, "x", "y", "")
	c := DeriveStepID("token", Owner{Kind: OwnerEpisode, EpisodeRef: ep1}, StepFileMove, "x", "z", "")
	d := DeriveStepID("other", Owner{Kind: OwnerEpisode, EpisodeRef: ep1}, StepFileMove, "x", "y", "")
	if a == b || a == c || a == d {
		t.Fatalf("collisions across distinct inputs: a=%s b=%s c=%s d=%s", a, b, c, d)
	}
}

func TestDeriveStepIDQuick(t *testing.T) {
	prop := func(token, from, to string) bool {
		ep, err := refs.NewEpisode(1, 1)
		if err != nil {
			return true
		}
		owner := Owner{Kind: OwnerEpisode, EpisodeRef: ep}
		got := DeriveStepID(token, owner, StepFileMove, from, to, "")
		if len(got) != 16 {
			return false
		}
		// idempotency
		again := DeriveStepID(token, owner, StepFileMove, from, to, "")
		return got == again
	}
	if err := quick.Check(prop, &quick.Config{MaxCount: 200}); err != nil {
		t.Fatal(err)
	}
}
