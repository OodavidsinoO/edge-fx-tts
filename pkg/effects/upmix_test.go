package effects

import "testing"

func TestUpmixBuild(t *testing.T) {
	n := buildNode(t, "upmix", nil)
	if err := n.Close(); err != nil {
		t.Fatal(err)
	}
}

func TestUpmixUnknownMode(t *testing.T) {
	if _, err := registry["upmix"](24000, map[string]any{"mode": "wide"}); err == nil {
		t.Fatal("want error for unknown mode")
	}
}

func TestUpmixFillsRightFromLeft(t *testing.T) {
	n := buildNode(t, "upmix", map[string]any{"mode": "center"})
	buf := stereoFrames(8)
	for i := 0; i < len(buf); i += 2 {
		buf[i] = float32(i/2 + 1) // L = 1,2,3,...
		buf[i+1] = -1             // R garbage, must be overwritten
	}
	if err := n.ProcessInPlace(buf); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < len(buf); i += 2 {
		if buf[i+1] != buf[i] {
			t.Fatalf("frame %d: R=%v want L=%v", i/2, buf[i+1], buf[i])
		}
	}
}

func TestUpmixZeroAllocs(t *testing.T) {
	n := buildNode(t, "upmix", nil)
	assertZeroAllocs(t, n, stereoFrames(4096))
}
