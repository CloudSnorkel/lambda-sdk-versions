package main

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestKeyStringRoundTrip(t *testing.T) {
	original := Key{Region: "us-east-1", Runtime: "nodejs22.x", Architecture: "arm64"}
	encoded := keyToString(original)
	decoded := stringToKey(encoded)

	if decoded != original {
		t.Fatalf("key round trip mismatch: got %+v want %+v", decoded, original)
	}
}

func TestReadJSONMissingFileReturnsEmptyResults(t *testing.T) {
	results, err := readJSON(filepath.Join(t.TempDir(), "does-not-exist.json"))
	if err != nil {
		t.Fatalf("expected no error for missing file, got %v", err)
	}

	if len(results) != 0 {
		t.Fatalf("expected empty results, got %d entries", len(results))
	}
}

func TestWriteThenReadJSONRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "results.json")
	now := time.Date(2026, 4, 8, 12, 0, 0, 0, time.UTC)

	input := Results{
		{Region: "us-east-1", Runtime: "python3.12", Architecture: "x86_64"}: {
			{Date: now, Version: "1.34.0"},
		},
	}

	writeJSON(path, input)

	if _, err := os.Stat(path); err != nil {
		t.Fatalf("expected file to be written, got error: %v", err)
	}

	got, err := readJSON(path)
	if err != nil {
		t.Fatalf("expected read to succeed, got error: %v", err)
	}

	if len(got) != len(input) {
		t.Fatalf("expected %d keys, got %d", len(input), len(got))
	}

	key := Key{Region: "us-east-1", Runtime: "python3.12", Architecture: "x86_64"}
	if len(got[key]) != 1 {
		t.Fatalf("expected one result entry, got %d", len(got[key]))
	}

	if got[key][0].Version != "1.34.0" {
		t.Fatalf("unexpected version: got %q", got[key][0].Version)
	}
	if !got[key][0].Date.Equal(now) {
		t.Fatalf("unexpected date: got %v want %v", got[key][0].Date, now)
	}
}
