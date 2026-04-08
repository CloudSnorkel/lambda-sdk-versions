package main

import (
	"archive/zip"
	"bytes"
	"testing"

	"github.com/aws/aws-sdk-go-v2/service/lambda/types"
)

func TestCreateZipIncludesFileAndContent(t *testing.T) {
	zipBytes := createZip("index.py", "print('hello')")

	r, err := zip.NewReader(bytes.NewReader(zipBytes), int64(len(zipBytes)))
	if err != nil {
		t.Fatalf("expected valid zip, got error: %v", err)
	}

	if len(r.File) != 1 {
		t.Fatalf("expected 1 file in zip, got %d", len(r.File))
	}

	if r.File[0].Name != "index.py" {
		t.Fatalf("expected file name index.py, got %s", r.File[0].Name)
	}

	f, err := r.File[0].Open()
	if err != nil {
		t.Fatalf("expected to open zip file entry, got error: %v", err)
	}
	defer f.Close()

	buf := new(bytes.Buffer)
	if _, err := buf.ReadFrom(f); err != nil {
		t.Fatalf("expected to read zip file entry, got error: %v", err)
	}

	if got, want := buf.String(), "print('hello')"; got != want {
		t.Fatalf("unexpected zip content: got %q want %q", got, want)
	}
}

func TestParseVersion(t *testing.T) {
	tests := []struct {
		name    string
		payload []byte
		want    string
		wantErr bool
	}{
		{
			name:    "success",
			payload: []byte(`{"version":"1.2.3"}`),
			want:    "1.2.3",
		},
		{
			name:    "missing version",
			payload: []byte(`{"message":"ok"}`),
			wantErr: true,
		},
		{
			name:    "invalid json",
			payload: []byte("not-json"),
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := parseVersion(tt.payload)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("expected error, got nil")
				}
				return
			}

			if err != nil {
				t.Fatalf("expected no error, got %v", err)
			}

			if got != tt.want {
				t.Fatalf("unexpected version: got %q want %q", got, tt.want)
			}
		})
	}
}

func TestRuntimeClassifiers(t *testing.T) {
	if !isNodejs(types.RuntimeNodejs22x) {
		t.Fatalf("expected nodejs22x to be nodejs runtime")
	}
	if !isPython3(types.RuntimePython312) {
		t.Fatalf("expected python3.12 to be python3 runtime")
	}
	if !isJava(types.RuntimeJava21) {
		t.Fatalf("expected java21 to be java runtime")
	}
	if !isRuby(types.RuntimeRuby34) {
		t.Fatalf("expected ruby3.4 to be ruby runtime")
	}
	if isRuby(types.RuntimeNodejs22x) {
		t.Fatalf("expected nodejs runtime to not be ruby")
	}
}
