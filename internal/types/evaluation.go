package types

import (
	"embed"
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/yanyiwu/gojieba"
)

// jiebaDictFS carries the cppjieba dictionaries inside the binary. The
// gojieba library defaults resolve dict paths from its compile-time module
// cache location, which only exists on the build machine; a distributed
// binary would panic at startup. Embedding them lets first use materialize
// the dicts into the per-user config directory instead.
//
//go:embed dicts
var jiebaDictFS embed.FS

// jiebaInstance lazily constructs the segmenter on first use so package
// initialization stays side-effect free (wails binding generation and other
// tools import this package without needing a working dictionary).
type jiebaInstance struct {
	once sync.Once
	j    *gojieba.Jieba
}

// Jieba is the global Chinese text segmentation tool (lazy).
var Jieba jiebaInstance

func (w *jiebaInstance) instance() *gojieba.Jieba {
	w.once.Do(func() { w.j = newJieba() })
	return w.j
}

func (w *jiebaInstance) Cut(s string, hmm bool) []string {
	return w.instance().Cut(s, hmm)
}

func (w *jiebaInstance) CutForSearch(s string, hmm bool) []string {
	return w.instance().CutForSearch(s, hmm)
}

func newJieba() *gojieba.Jieba {
	dictDir := os.Getenv("JIEBA_DICT_DIR")
	if dictDir == "" {
		// Dev machines build from a module cache, so the library defaults
		// (resolved from the compiled-in source path) exist there.
		if _, err := os.Stat(gojieba.DICT_PATH); err == nil {
			return gojieba.NewJieba()
		}
		dictDir = materializeJiebaDicts()
	}
	return gojieba.NewJieba(
		filepath.Join(dictDir, "jieba.dict.utf8"),
		filepath.Join(dictDir, "hmm_model.utf8"),
		filepath.Join(dictDir, "user.dict.utf8"),
		filepath.Join(dictDir, "idf.utf8"),
		filepath.Join(dictDir, "stop_words.utf8"),
	)
}

// materializeJiebaDicts writes the embedded dictionaries into the per-user
// config directory (skipping files already present with the expected size)
// and returns that directory.
func materializeJiebaDicts() string {
	base, err := os.UserConfigDir()
	if err != nil || base == "" {
		base = os.TempDir()
	}
	dictDir := filepath.Join(base, "WeKnora Lite", "jieba")
	entries, err := fs.ReadDir(jiebaDictFS, "dicts")
	if err != nil {
		panic(fmt.Sprintf("read embedded jieba dicts: %v", err))
	}
	if err := os.MkdirAll(dictDir, 0o755); err != nil {
		panic(fmt.Sprintf("create jieba dict dir: %v", err))
	}
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		dst := filepath.Join(dictDir, e.Name())
		var want int64
		if fi, err := e.Info(); err == nil {
			want = fi.Size()
		}
		if fi, err := os.Stat(dst); err == nil && want > 0 && fi.Size() == want {
			continue
		}
		data, err := jiebaDictFS.ReadFile("dicts/" + e.Name())
		if err != nil {
			panic(fmt.Sprintf("read embedded jieba dict %s: %v", e.Name(), err))
		}
		if err := os.WriteFile(dst, data, 0o644); err != nil {
			panic(fmt.Sprintf("write jieba dict %s: %v", e.Name(), err))
		}
	}
	return dictDir
}

// EvaluationStatue represents the status of an evaluation task
type EvaluationStatue int

const (
	EvaluationStatuePending EvaluationStatue = iota // Task is waiting to start
	EvaluationStatueRunning                         // Task is in progress
	EvaluationStatueSuccess                         // Task completed successfully
	EvaluationStatueFailed                          // Task failed
)

// EvaluationTask contains information about an evaluation task
type EvaluationTask struct {
	ID        string `json:"id"`         // Unique task ID
	TenantID  uint64 `json:"tenant_id"`  // Tenant/Organization ID
	DatasetID string `json:"dataset_id"` // Dataset ID for evaluation

	StartTime time.Time        `json:"start_time"`        // Task start time
	Status    EvaluationStatue `json:"status"`            // Current task status
	ErrMsg    string           `json:"err_msg,omitempty"` // Error message if failed

	Total    int `json:"total,omitempty"`    // Total items to evaluate
	Finished int `json:"finished,omitempty"` // Completed items count
}

// EvaluationDetail contains detailed evaluation information
type EvaluationDetail struct {
	Task   *EvaluationTask `json:"task"`             // Evaluation task info
	Params *ChatManage     `json:"params"`           // Evaluation parameters
	Metric *MetricResult   `json:"metric,omitempty"` // Evaluation metrics
}

// String returns JSON representation of EvaluationTask
func (e *EvaluationTask) String() string {
	b, _ := json.Marshal(e)
	return string(b)
}

// MetricInput contains input data for metric calculation
type MetricInput struct {
	RetrievalGT  [][]int // Ground truth for retrieval
	RetrievalIDs []int   // Retrieved IDs

	GeneratedTexts string // Generated text for evaluation
	GeneratedGT    string // Ground truth text for comparison
}

// MetricResult contains evaluation metrics
type MetricResult struct {
	RetrievalMetrics  RetrievalMetrics  `json:"retrieval_metrics"`  // Retrieval performance metrics
	GenerationMetrics GenerationMetrics `json:"generation_metrics"` // Text generation quality metrics
}

// RetrievalMetrics contains metrics for retrieval evaluation
type RetrievalMetrics struct {
	Precision float64 `json:"precision"` // Precision score
	Recall    float64 `json:"recall"`    // Recall score

	NDCG3  float64 `json:"ndcg3"`  // Normalized Discounted Cumulative Gain at 3
	NDCG10 float64 `json:"ndcg10"` // Normalized Discounted Cumulative Gain at 10
	MRR    float64 `json:"mrr"`    // Mean Reciprocal Rank
	MAP    float64 `json:"map"`    // Mean Average Precision
}

// GenerationMetrics contains metrics for text generation evaluation
type GenerationMetrics struct {
	BLEU1 float64 `json:"bleu1"` // BLEU-1 score
	BLEU2 float64 `json:"bleu2"` // BLEU-2 score
	BLEU4 float64 `json:"bleu4"` // BLEU-4 score

	ROUGE1 float64 `json:"rouge1"` // ROUGE-1 score
	ROUGE2 float64 `json:"rouge2"` // ROUGE-2 score
	ROUGEL float64 `json:"rougel"` // ROUGE-L score
}

// EvalState represents different stages of evaluation process
type EvalState int

const (
	StateBegin             EvalState = iota // Evaluation started
	StateAfterQaPairs                       // After loading QA pairs
	StateAfterDataset                       // After processing dataset
	StateAfterEmbedding                     // After generating embeddings
	StateAfterVectorSearch                  // After vector search
	StateAfterRerank                        // After reranking
	StateAfterComplete                      // After completion
	StateEnd                                // Evaluation ended
)
