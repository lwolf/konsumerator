package consumer

import (
	"path/filepath"
	"testing"
)

func TestGetCpuRequest(t *testing.T) {
	fname := "testdata/cpu.shares"
	t.Log(filepath.Abs(fname))
	value, err := getCpuRequest(fname)
	if err != nil {
		t.Fatal(err)
	}
	t.Log(value)
}
