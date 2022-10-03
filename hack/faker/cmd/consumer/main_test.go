package consumer

import (
	"path/filepath"
	"testing"
)

func TestGetCpuRequest(t *testing.T) {
	fname := "../../testdata/cpu.shares"
	value, err := getCpuRequest(fname)
	if err != nil {
		t.Log(filepath.Abs(fname))
		t.Fatal(err)
	}
	t.Log(value)
}
