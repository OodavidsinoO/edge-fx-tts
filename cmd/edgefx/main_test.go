package main

import (
	"bytes"
	"errors"
	"io"
	"os"
	"path/filepath"
	"testing"
)

func TestWriteReaderToFileWritesBytes(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "out.mp3")
	input := []byte("ID3\x00\x01fake mp3 bytes")

	if err := writeReaderToFile(bytes.NewReader(input), path); err != nil {
		t.Fatalf("writeReaderToFile returned error: %v", err)
	}

	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read output: %v", err)
	}
	if !bytes.Equal(got, input) {
		t.Fatalf("content mismatch: got %d bytes, want %d", len(got), len(input))
	}
}

func TestWriteReaderToFileRemovesPartialOnError(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "out.mp3")

	// A reader that fails after emitting bytes.
	errReader := &errorReader{data: []byte("partial"), err: errors.New("boom")}

	err := writeReaderToFile(errReader, path)
	if err == nil {
		t.Fatalf("expected error, got nil")
	}
	if _, statErr := os.Stat(path); !os.IsNotExist(statErr) {
		t.Fatalf("partial output file still exists after error: %v", statErr)
	}
}

func TestHasSSMLFlag(t *testing.T) {
	cases := []struct {
		args []string
		want bool
	}{
		{[]string{"ssml"}, true},
		{[]string{"SSML"}, true},
		{[]string{"text"}, false},
		{nil, false},
		{[]string{"ssml", "-o", "x.mp3"}, true},
	}
	for _, c := range cases {
		if got := hasSSMLFlag(c.args); got != c.want {
			t.Fatalf("hasSSMLFlag(%q) = %v, want %v", c.args, got, c.want)
		}
	}
}

type errorReader struct {
	data []byte
	err  error
}

func (r *errorReader) Read(p []byte) (int, error) {
	if len(p) == 0 {
		return 0, nil
	}
	if len(r.data) == 0 {
		if r.err != nil {
			return 0, r.err
		}
		return 0, io.EOF
	}
	// 一次调用返回全部剩余数据并在之后返回 err。
	n := copy(p, r.data)
	r.data = r.data[n:]
	if len(r.data) == 0 && r.err != nil {
		return n, r.err
	}
	return n, nil
}
