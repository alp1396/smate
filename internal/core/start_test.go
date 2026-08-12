package core

import "testing"

// The id becomes both a directory name and a branch name, so it is checked
// before anything is created.
func TestValidateID(t *testing.T) {
	for _, id := range []string{"123", "task-1", "fix_bug.2", "PROJ-7"} {
		if err := validateID(id); err != nil {
			t.Errorf("%q should be accepted: %v", id, err)
		}
	}
	for _, id := range []string{"", ".", "..", "a/b", `a\b`, "a b", "a:b", "a~b", "a^b", "a?b", "a*b", "a[b", "-x"} {
		if err := validateID(id); err == nil {
			t.Errorf("%q should be rejected", id)
		}
	}
}
