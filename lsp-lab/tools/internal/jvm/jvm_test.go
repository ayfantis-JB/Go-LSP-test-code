package jvm

import (
	"os"
	"path/filepath"
	"testing"
)

func TestParseHeapInfo(t *testing.T) {
	g1 := ` garbage-first heap   total reserved 2097152K, committed 262144K, used 104857K [0x0000000780000000, 0x0000000800000000)
  region size 1024K, 37 young (37888K), 4 survivors (4096K)`
	h, err := ParseHeapInfo(g1)
	if err != nil {
		t.Fatal(err)
	}
	if h.CommittedMB != 256 || h.UsedMB < 102 || h.UsedMB > 103 {
		t.Errorf("got %+v", h)
	}
	if _, err := ParseHeapInfo("Native memory tracking is not enabled"); err == nil {
		t.Error("expected error")
	}
}

func TestDiffHistograms(t *testing.T) {
	dir := t.TempDir()
	write := func(name, body string) string {
		p := filepath.Join(dir, name)
		if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
		return p
	}
	a := write("a.txt", " num     #instances         #bytes  class name\n   1:        10        1000  [B\n   2:         5         500  java.lang.String\n")
	b := write("b.txt", "   1:        50        9000  [B\n   2:         5         500  java.lang.String\n   3:         1          64  com.example.Leak\n")
	ha, _ := ReadHistogram(a)
	hb, _ := ReadHistogram(b)
	d := DiffHistograms(ha, hb)
	if d[0].Class != "[B" || d[0].DeltaBytes != 8000 || d[0].DeltaInst != 40 {
		t.Errorf("top delta = %+v", d[0])
	}
	if d[1].Class != "com.example.Leak" {
		t.Errorf("second = %+v", d[1])
	}
}
