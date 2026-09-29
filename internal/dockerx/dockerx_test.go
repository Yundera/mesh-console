package dockerx

import (
	"testing"
	"time"
)

func TestParseHandshakes(t *testing.T) {
	out := "wg0\tAAAApeerkey=\t1790000000\nwg1\tBBBBpeer=\t0\nnoise\n"
	hs := ParseHandshakes(out)
	if len(hs) != 2 {
		t.Fatalf("got %d", len(hs))
	}
	if hs[0].Interface != "wg0" || !hs[0].Last.Equal(time.Unix(1790000000, 0)) || hs[0].Never {
		t.Fatalf("hs[0] = %+v", hs[0])
	}
	if !hs[1].Never {
		t.Fatalf("hs[1] = %+v", hs[1])
	}
}

func TestHealthFromStatus(t *testing.T) {
	for in, want := range map[string]string{
		"Up 2 hours (healthy)":           "healthy",
		"Up 1 second (health: starting)": "starting",
		"Up 3 days (unhealthy)":          "unhealthy",
		"Up 3 days":                      "",
	} {
		if got := healthFromStatus(in); got != want {
			t.Errorf("%q -> %q, want %q", in, got, want)
		}
	}
}
