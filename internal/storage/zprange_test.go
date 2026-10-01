package storage

import (
	"archive/zip"
	"bytes"
	"io"
	"math/rand/v2"
	"os"
	"path/filepath"
	"testing"
)

func buildPlan(t *testing.T) (*ZipPlan, []byte) {
	t.Helper()

	root := t.TempDir()
	sizes := map[string]int{"a.bin": 40000, "sub/b.bin": 1, "sub/c.bin": 0, "d.bin": 131072}
	for name, size := range sizes {
		p := filepath.Join(root, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		body := make([]byte, size)
		for i := range body {
			body[i] = byte(rand.UintN(256))
		}
		if err := os.WriteFile(p, body, 0o644); err != nil {
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

	var full bytes.Buffer
	if err := plan.Stream(&full); err != nil {
		t.Fatal(err)
	}
	if int64(full.Len()) != plan.Size {
		t.Fatalf("full stream wrote %d, planned %d", full.Len(), plan.Size)
	}

	return plan, full.Bytes()
}

// TestStreamRangeMatchesFullStream is the contract a resume depends on:
// any span of the archive must equal that span of a single full download.
func TestStreamRangeMatchesFullStream(t *testing.T) {
	plan, full := buildPlan(t)
	size := plan.Size

	spans := [][2]int64{
		{0, size - 1},
		{0, 0},
		{size - 1, size - 1},
		{1, 29},
		{size / 2, size - 1},
		{size - 300, size - 1},
		{40000, 40100},
		{size/3 + 7, size/3 + 8},
	}

	for _, span := range spans {
		var got bytes.Buffer
		if err := plan.StreamRange(&got, span[0], span[1]); err != nil {
			t.Fatalf("StreamRange(%d, %d): %v", span[0], span[1], err)
		}

		want := full[span[0] : span[1]+1]
		if !bytes.Equal(got.Bytes(), want) {
			t.Errorf("StreamRange(%d, %d) wrote %d bytes, want %d matching the full stream",
				span[0], span[1], got.Len(), len(want))
		}
	}
}

// TestResumedArchiveOpens stitches an interrupted download back together
// the way a browser does and checks the result is a valid zip.
func TestResumedArchiveOpens(t *testing.T) {
	plan, full := buildPlan(t)
	cut := plan.Size / 3

	var head, tail bytes.Buffer
	if err := plan.StreamRange(&head, 0, cut-1); err != nil {
		t.Fatal(err)
	}
	if err := plan.StreamRange(&tail, cut, plan.Size-1); err != nil {
		t.Fatal(err)
	}

	joined := append(head.Bytes(), tail.Bytes()...)
	if !bytes.Equal(joined, full) {
		t.Fatalf("resumed archive differs from a single download")
	}

	zr, err := zip.NewReader(bytes.NewReader(joined), int64(len(joined)))
	if err != nil {
		t.Fatalf("resumed archive does not open: %v", err)
	}
	if len(zr.File) != len(plan.Entries) {
		t.Fatalf("resumed archive holds %d files, want %d", len(zr.File), len(plan.Entries))
	}
	for _, f := range zr.File {
		rc, err := f.Open()
		if err != nil {
			t.Fatalf("%s: %v", f.Name, err)
		}
		// The whole member has to be read for Close to verify its CRC,
		// which is what proves the data descriptors survived the splice.
		if _, err := io.Copy(io.Discard, rc); err != nil {
			t.Fatalf("%s: %v", f.Name, err)
		}
		if err := rc.Close(); err != nil {
			t.Fatalf("%s: %v", f.Name, err)
		}
	}
}
