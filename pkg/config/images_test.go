package config

import (
	"testing"
)

func TestImagesByGroup(t *testing.T) {
	groups, err := ImagesByGroup()
	if err != nil {
		t.Fatalf("ImagesByGroup() failed: %v", err)
	}

	if len(groups) == 0 {
		t.Error("expected groups to be non-empty")
	}

	for name, images := range groups {
		if len(images) == 0 {
			t.Errorf("group %s has no images", name)
		}
	}
}
