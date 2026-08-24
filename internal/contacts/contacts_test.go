package contacts

import (
	"os"
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

func TestDeriveName(t *testing.T) {
	cases := map[string]string{
		"example.name@domain.io":  "Example Name",
		"jean_pierre.dupont@x.fr": "Jean Pierre Dupont",
		"simon@ssp.sh":            "", // single segment — could be anything
		"no-reply@x.io":           "", // role mailbox
		"info.team@x.io":          "", // role mailbox
		"a.b2c@x.io":              "", // digits — not a person pattern
		"not-an-address":          "",
	}
	for addr, want := range cases {
		if got := DeriveName(addr); got != want {
			t.Errorf("DeriveName(%q) = %q, want %q", addr, got, want)
		}
	}
}

func TestMergeFileSimpleFormat(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "contacts.txt")
	content := "# my contacts\n" +
		"lnachname@domain.io,Louise Nachname\n" +
		"tabbed@x.io\tTab Name\n" +
		"Angle Form <angle@x.io>\n"
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	s := Load(filepath.Join(dir, "cache"))
	if err := s.MergeFile(path); err != nil {
		t.Fatalf("MergeFile: %v", err)
	}
	for addr, want := range map[string]string{
		"lnachname@domain.io": "Louise Nachname",
		"tabbed@x.io":         "Tab Name",
		"angle@x.io":          "Angle Form",
	} {
		if got := s.Name(addr); got != want {
			t.Errorf("Name(%s) = %q, want %q", addr, got, want)
		}
	}
	// Missing file must be a silent no-op — the feature is optional.
	if err := s.MergeFile(filepath.Join(dir, "missing.csv")); err != nil {
		t.Errorf("missing file: %v", err)
	}
}

func TestMergeFileGoogleCSV(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "google.csv")
	content := `Name,First Name,Last Name,E-mail 1 - Value,E-mail 2 - Value
Louise Nachname,Louise,Nachname,lnachname@domain.io,l.private@x.io ::: l.work@x.io
,Bob,Builder,bob@x.io,
`
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	s := Load(filepath.Join(dir, "cache"))
	if err := s.MergeFile(path); err != nil {
		t.Fatalf("MergeFile: %v", err)
	}
	for addr, want := range map[string]string{
		"lnachname@domain.io": "Louise Nachname",
		"l.private@x.io":      "Louise Nachname",
		"l.work@x.io":         "Louise Nachname",
		"bob@x.io":            "Bob Builder", // Name column empty → First + Last
	} {
		if got := s.Name(addr); got != want {
			t.Errorf("Name(%s) = %q, want %q", addr, got, want)
		}
	}
}

func TestDecorateFallsBackToDerivedName(t *testing.T) {
	s := Load(filepath.Join(t.TempDir(), "cache"))
	got := s.Decorate("example.name@domain.io, simon@ssp.sh")
	want := "Example Name <example.name@domain.io>, simon@ssp.sh"
	if got != want {
		t.Errorf("Decorate = %q, want %q", got, want)
	}
}
