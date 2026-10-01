package storage

import (
	"os"
	"path/filepath"
	"testing"
)

func TestResolveRejectsEscapes(t *testing.T) {
	outside := t.TempDir()
	if err := os.WriteFile(filepath.Join(outside, "secret.txt"), []byte("no"), 0o644); err != nil {
		t.Fatal(err)
	}

	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "sub"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "sub", "ok.txt"), []byte("yes"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(root, "link")); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(outside, "secret.txt"), filepath.Join(root, "leak.txt")); err != nil {
		t.Fatal(err)
	}

	r, err := NewRoot(root)
	if err != nil {
		t.Fatal(err)
	}

	denied := []string{
		"../",
		"../../etc/passwd",
		"/etc/passwd",
		"sub/../../",
		`..\..\windows`,
		"link/secret.txt",
		"leak.txt",
	}
	for _, rel := range denied {
		if got, err := r.Resolve(rel); err == nil {
			t.Errorf("Resolve(%q) allowed %q, want refusal", rel, got)
		}
	}

	// Compared against r.Dir(), not root: on macOS /var is itself a
	// symlink, so the root the server works from is the resolved one.
	allowed := map[string]string{
		"":             r.Dir(),
		".":            r.Dir(),
		"sub":          filepath.Join(r.Dir(), "sub"),
		"/sub/ok.txt":  filepath.Join(r.Dir(), "sub", "ok.txt"),
		"sub/./ok.txt": filepath.Join(r.Dir(), "sub", "ok.txt"),
	}
	for rel, want := range allowed {
		got, err := r.Resolve(rel)
		if err != nil {
			t.Errorf("Resolve(%q) = error %v, want %q", rel, err, want)
			continue
		}
		if got != want {
			t.Errorf("Resolve(%q) = %q, want %q", rel, got, want)
		}
	}
}

func TestPlanZipSkipsSymlinks(t *testing.T) {
	outside := t.TempDir()
	if err := os.WriteFile(filepath.Join(outside, "secret.txt"), []byte("no"), 0o644); err != nil {
		t.Fatal(err)
	}

	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "real.txt"), []byte("yes"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(outside, "secret.txt"), filepath.Join(root, "leak.txt")); err != nil {
		t.Fatal(err)
	}

	r, err := NewRoot(root)
	if err != nil {
		t.Fatal(err)
	}

	plan, err := r.PlanZip([]string{""})
	if err != nil {
		t.Fatal(err)
	}

	if len(plan.Entries) != 1 || plan.Entries[0].Name != "real.txt" {
		t.Fatalf("archive holds %v, want only real.txt", plan.Entries)
	}
}

func TestNeededSkipsMatchingSizes(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "trip"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "trip", "a.jpg"), make([]byte, 100), 0o644); err != nil {
		t.Fatal(err)
	}

	got := NewSaver(dir).Needed(nil)
	if len(got) != 0 {
		t.Fatalf("empty manifest needed %v", got)
	}

	got = NewSaver(dir).Needed([]ManifestItem{
		{Path: "trip/a.jpg", Size: 100},
		{Path: "trip/a.jpg", Size: 101},
		{Path: "trip/b.jpg", Size: 1},
		{Path: "../escape.jpg", Size: 1},
	})

	want := []string{"trip/a.jpg", "trip/b.jpg"}
	if len(got) != len(want) {
		t.Fatalf("Needed = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("Needed = %v, want %v", got, want)
		}
	}
}
