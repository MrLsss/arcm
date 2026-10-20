package arc_test

import (
	"os"
	"testing"

	"arcm/internal/arc"
	"arcm/internal/model"
)

func TestReadRealArcStore(t *testing.T) {
	if os.Getenv("ARCM_TEST_REAL") != "1" {
		t.Skip("set ARCM_TEST_REAL=1 to read the live Arc store")
	}
	s, err := arc.Read("")
	if err != nil {
		t.Skipf("Arc store unavailable: %v", err)
	}
	if len(s.Spaces) != 1 {
		t.Fatalf("spaces = %d, want 1", len(s.Spaces))
	}
	links, _ := model.Count(s.Spaces[0].Pinned)
	if links == 0 {
		t.Fatal("live Arc store has no pinned links")
	}
	topLinks, _ := model.Count(s.Spaces[0].TopApps)
	if topLinks == 0 {
		t.Fatal("live Arc store has no Top Apps")
	}
}
