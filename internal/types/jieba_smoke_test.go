package types

import (
	"os"
	"path/filepath"
	"testing"
)

// TestJiebaSmoke validates the distributed-binary path: embedded dicts
// materialize to disk and gojieba loads them from a plain filesystem path
// (no module cache involved).
func TestJiebaSmoke(t *testing.T) {
	dir := materializeJiebaDicts()
	for _, name := range []string{
		"jieba.dict.utf8",
		"hmm_model.utf8",
		"user.dict.utf8",
		"idf.utf8",
		"stop_words.utf8",
	} {
		fi, err := os.Stat(filepath.Join(dir, name))
		if err != nil {
			t.Fatalf("dict %s not materialized: %v", name, err)
		}
		if fi.Size() == 0 {
			t.Fatalf("dict %s is empty", name)
		}
	}

	t.Setenv("JIEBA_DICT_DIR", dir)
	j := newJieba()
	words := j.CutForSearch("我来到北京清华大学", true)
	if len(words) == 0 {
		t.Fatal("CutForSearch returned no words")
	}
	t.Logf("words: %v", words)
}
