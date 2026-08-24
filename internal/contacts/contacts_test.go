package contacts

import (
	"path/filepath"
	"reflect"
	"testing"
)

func TestHarvestNameAndDecorate(t *testing.T) {
	s := Load(filepath.Join(t.TempDir(), "contacts"))
	s.HarvestField("Louise Nachname <lnachname@domain.io>, bare@domain.io")
	s.HarvestField("Simon Späti <simon@ssp.sh>")

	if got := s.Name("LNachname@domain.io"); got != "Louise Nachname" {
		t.Errorf("Name lookup = %q, want %q", got, "Louise Nachname")
	}
	if got := s.Name("bare@domain.io"); got != "" {
		t.Errorf("bare address must have no name, got %q", got)
	}

	// Bare addresses gain the known name; already-named and unknown parts stay.
	got := s.Decorate("lnachname@domain.io, Kept Name <k@x.io>, unknown@x.io")
	want := "Louise Nachname <lnachname@domain.io>, Kept Name <k@x.io>, unknown@x.io"
	if got != want {
		t.Errorf("Decorate = %q, want %q", got, want)
	}

	// Non-ASCII names are Q-encoded so headers stay RFC 2047 clean.
	if got := s.Decorate("simon@ssp.sh"); got != "=?utf-8?q?Simon_Sp=C3=A4ti?= <simon@ssp.sh>" {
		t.Errorf("Decorate non-ASCII = %q", got)
	}
}

func TestAddRejectsUnsafeNames(t *testing.T) {
	s := Load(filepath.Join(t.TempDir(), "contacts"))
	s.Add("a@b.io", "Nachname, Louise") // comma would break field splitting
	s.Add("c@b.io", `Evil <x>`)
	s.Add("d@b.io", "d@b.io") // name identical to address is noise
	for _, addr := range []string{"a@b.io", "c@b.io", "d@b.io"} {
		if got := s.Name(addr); got != "" {
			t.Errorf("unsafe name for %s stored: %q", addr, got)
		}
	}
}

func TestAddrsMatchingNameAndPersistence(t *testing.T) {
	path := filepath.Join(t.TempDir(), "contacts")
	s := Load(path)
	s.Add("lnachname@domain.io", "Louise Nachname")
	s.Add("louis@other.io", "Louis Other")
	s.Add("bob@x.io", "Bob")
	if err := s.SaveIfDirty(); err != nil {
		t.Fatalf("save: %v", err)
	}

	s2 := Load(path)
	got := s2.AddrsMatchingName("louis", 5)
	want := []string{"lnachname@domain.io", "louis@other.io"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("AddrsMatchingName = %v, want %v", got, want)
	}
	if got := s2.AddrsMatchingName("louis", 1); len(got) != 1 {
		t.Errorf("limit not applied: %v", got)
	}
}

func TestNilStoreIsSafe(t *testing.T) {
	var s *Store
	s.Add("a@b.io", "X")
	s.HarvestField("A <a@b.io>")
	if s.Name("a@b.io") != "" || s.AddrsMatchingName("a", 5) != nil {
		t.Error("nil store must return zero values")
	}
	if got := s.Decorate("a@b.io"); got != "a@b.io" {
		t.Errorf("nil Decorate = %q", got)
	}
	if err := s.SaveIfDirty(); err != nil {
		t.Errorf("nil SaveIfDirty: %v", err)
	}
}
