package patch

import (
	"reflect"
	"testing"
)

func TestScanValuesFindsLeakedKeys(t *testing.T) {
	keys := map[string]string{
		"OPENAI_API_KEY":     "sk-proj-0123456789abcdef",
		"OPENROUTER_API_KEY": "sk-or-v1-fedcba9876543210",
		"UNUSED_KEY":         "sk-nothing-of-the-sort",
	}
	data := []byte("diff --git a/conf.php b/conf.php\n+$key = 'sk-proj-0123456789abcdef';\n")

	got := ScanValues(data, keys)
	if want := []string{"OPENAI_API_KEY"}; !reflect.DeepEqual(got, want) {
		t.Errorf("got %v, want %v", got, want)
	}
}

func TestScanValuesReportsEveryLeak(t *testing.T) {
	keys := map[string]string{
		"A_KEY": "aaaaaaaaaaaa",
		"B_KEY": "bbbbbbbbbbbb",
	}
	data := []byte("+aaaaaaaaaaaa\n+bbbbbbbbbbbb\n")
	if got := ScanValues(data, keys); !reflect.DeepEqual(got, []string{"A_KEY", "B_KEY"}) {
		t.Errorf("got %v, want both keys", got)
	}
}

func TestScanValuesCleanPatch(t *testing.T) {
	keys := map[string]string{"OPENAI_API_KEY": "sk-proj-0123456789abcdef"}
	data := []byte("diff --git a/a.txt b/a.txt\n+just a change\n")
	if got := ScanValues(data, keys); len(got) != 0 {
		t.Errorf("a clean patch must not trip the scan: %v", got)
	}
}

// A short value would match half the world, so it is ignored on purpose.
func TestScanValuesIgnoresShortValues(t *testing.T) {
	keys := map[string]string{"WEAK": "test"}
	data := []byte("+ this is a test line\n")
	if got := ScanValues(data, keys); len(got) != 0 {
		t.Errorf("short values must be ignored, got %v", got)
	}
}

func TestScanValuesNoKeys(t *testing.T) {
	if got := ScanValues([]byte("anything"), nil); len(got) != 0 {
		t.Errorf("no keys configured means nothing to find, got %v", got)
	}
}
