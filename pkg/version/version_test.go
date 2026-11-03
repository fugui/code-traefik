package version

import (
	"strings"
	"testing"
)

func TestVersionGetAndShort(t *testing.T) {
	info := Get()
	if info.Version != Version {
		t.Fatalf("expected version %q, got %q", Version, info.Version)
	}
	if info.CommitID != CommitID {
		t.Fatalf("expected commit %q, got %q", CommitID, info.CommitID)
	}
	if info.BuildTime != BuildTime {
		t.Fatalf("expected buildTime %q, got %q", BuildTime, info.BuildTime)
	}

	if Short() != Version {
		t.Fatalf("expected Short() to be %q, got %q", Version, Short())
	}
}

func TestVersionFullAndString(t *testing.T) {
	full := Full()
	if !strings.Contains(full, Version) {
		t.Fatalf("expected Full() to contain version %q, got %q", Version, full)
	}
	if !strings.Contains(full, CommitID) {
		t.Fatalf("expected Full() to contain commit %q, got %q", CommitID, full)
	}
	if !strings.Contains(full, BuildTime) {
		t.Fatalf("expected Full() to contain buildTime %q, got %q", BuildTime, full)
	}

	if String() != full {
		t.Fatalf("expected String() to equal Full(), got %q vs %q", String(), full)
	}
}
