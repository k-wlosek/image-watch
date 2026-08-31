package secret

import (
	"os"
	"path/filepath"
	"testing"
)

func TestReadFile_HappyPath(t *testing.T) {
	path := filepath.Join(t.TempDir(), "secret.txt")
	if err := os.WriteFile(path, []byte("my-token\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	got, err := ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile error: %v", err)
	}
	if got != "my-token" {
		t.Errorf("ReadFile = %q, want %q", got, "my-token")
	}
}

func TestReadFile_TrimsWhitespace(t *testing.T) {
	path := filepath.Join(t.TempDir(), "secret.txt")
	if err := os.WriteFile(path, []byte("  token-with-spaces  \n\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	got, err := ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile error: %v", err)
	}
	if got != "token-with-spaces" {
		t.Errorf("ReadFile = %q, want %q", got, "token-with-spaces")
	}
}

func TestReadFile_EmptyFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "secret.txt")
	if err := os.WriteFile(path, []byte(""), 0o600); err != nil {
		t.Fatal(err)
	}
	got, err := ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile error: %v", err)
	}
	if got != "" {
		t.Errorf("ReadFile = %q, want empty", got)
	}
}

func TestReadFile_MissingFile(t *testing.T) {
	_, err := ReadFile(filepath.Join(t.TempDir(), "nonexistent"))
	if err == nil {
		t.Fatal("expected error for missing file")
	}
}

func TestReadFile_DirectoryInsteadOfFile(t *testing.T) {
	_, err := ReadFile(t.TempDir())
	if err == nil {
		t.Fatal("expected error when path is a directory")
	}
}
