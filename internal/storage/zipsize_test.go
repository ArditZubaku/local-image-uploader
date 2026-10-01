package storage

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestZipSizeMatchesWriter pins the Content-Length formula to what
// archive/zip actually emits. If a Go upgrade changes the writer's header
// or extra-field layout, this fails here rather than as truncated
// downloads on someone's phone.
func TestZipSizeMatchesWriter(t *testing.T) {
	cases := []struct {
		name  string
		files map[string]int
	}{
		{"empty", nil},
		{"single", map[string]int{"a.jpg": 1234}},
		{"zero byte", map[string]int{"empty.txt": 0}},
		{"nested", map[string]int{"a.jpg": 10, "sub/b.png": 2048, "sub/deep/c.mov": 70000}},
		{"unicode name", map[string]int{"fotoğraf â€” ünïcode.jpg": 99}},
		{"long name", map[string]int{strings.Repeat("x", 200) + ".bin": 7}},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			root := t.TempDir()
			for name, size := range tc.files {
				p := filepath.Join(root, filepath.FromSlash(name))
				if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(p, make([]byte, size), 0o644); err != nil {
					t.Fatal(err)
				}
			}

			r, err := NewRoot(root)
			if err != nil {
				t.Fatal(err)
			}

			plan, err := r.PlanZip([]string{""})
			if err != nil {
				t.Fatal(err)
			}
			if got := len(plan.Entries); got != len(tc.files) {
				t.Fatalf("planned %d entries, want %d", got, len(tc.files))
			}

			var counted countingWriter
			if err := plan.Stream(&counted); err != nil {
				t.Fatal(err)
			}

			if counted.n != plan.Size {
				t.Errorf("wrote %d bytes, Content-Length would claim %d", counted.n, plan.Size)
			}
		})
	}
}

// TestZipSizeManyEntries covers the zip64 end-of-central-directory branch,
// which only triggers past 65535 members.
func TestZipSizeManyEntries(t *testing.T) {
	if testing.Short() {
		t.Skip("writes 65536 files")
	}

	root := t.TempDir()
	for i := range 65536 {
		if err := os.WriteFile(filepath.Join(root, fmt.Sprintf("f%05d", i)), nil, 0o644); err != nil {
			t.Fatal(err)
		}
	}

	r, err := NewRoot(root)
	if err != nil {
		t.Fatal(err)
	}
	plan, err := r.PlanZip([]string{""})
	if err != nil {
		t.Fatal(err)
	}

	var counted countingWriter
	if err := plan.Stream(&counted); err != nil {
		t.Fatal(err)
	}
	if counted.n != plan.Size {
		t.Errorf("wrote %d bytes, Content-Length would claim %d", counted.n, plan.Size)
	}
}

// TestZipShrinkingFileStaysExact guards the Content-Length contract when a
// file is truncated between planning and streaming.
func TestZipShrinkingFileStaysExact(t *testing.T) {
	root := t.TempDir()
	p := filepath.Join(root, "shrinks.bin")
	if err := os.WriteFile(p, make([]byte, 4096), 0o644); err != nil {
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

	if err := os.WriteFile(p, []byte("short"), 0o644); err != nil {
		t.Fatal(err)
	}

	var counted countingWriter
	if err := plan.Stream(&counted); err != nil {
		t.Fatal(err)
	}
	if counted.n != plan.Size {
		t.Errorf("wrote %d bytes, Content-Length would claim %d", counted.n, plan.Size)
	}
}

type countingWriter struct{ n int64 }

func (c *countingWriter) Write(p []byte) (int, error) {
	c.n += int64(len(p))
	return len(p), nil
}
