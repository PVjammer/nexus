// Package sheets is the built-in "local Sheets" app: CSV files that you and
// the agent both edit. The file is the source of truth; every write is atomic
// and goes through here. Conflicts are rev-based (content hash), not mtime.
// Sort/filter are view state, not file edits.
package sheets

import (
	"bytes"
	"crypto/sha256"
	"encoding/csv"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"sync"

	"github.com/pvjammer/nexus/prototypes/ux/internal/files"
)

var ErrStale = errors.New("stale: the file changed since you loaded it")

type Sheet struct {
	Path    string     `json:"path"`
	Rev     string     `json:"rev"`
	Headers []string   `json:"headers"`
	Rows    [][]string `json:"rows"`
}

type Op struct {
	Op    string   `json:"op"` // set | addRow | deleteRow | addCol | renameCol | deleteCol
	Row   int      `json:"row"`
	Col   int      `json:"col"`
	Value string   `json:"value"`
	Cells []string `json:"cells,omitempty"` // addRow
	At    *int     `json:"at,omitempty"`    // addRow: insert index (default append)
}

type Store struct {
	roots *files.Roots
	mu    sync.Mutex
}

func New(r *files.Roots) *Store { return &Store{roots: r} }

func rev(b []byte) string {
	h := sha256.Sum256(b)
	return hex.EncodeToString(h[:6])
}

func (s *Store) Load(path string) (*Sheet, error) {
	abs, err := s.roots.Resolve(path)
	if err != nil {
		return nil, err
	}
	b, err := os.ReadFile(abs)
	if err != nil {
		return nil, err
	}
	return parse(path, b)
}

func parse(path string, b []byte) (*Sheet, error) {
	r := csv.NewReader(bytes.NewReader(b))
	r.FieldsPerRecord = -1
	recs, err := r.ReadAll()
	if err != nil {
		return nil, fmt.Errorf("parse %s: %w", path, err)
	}
	sh := &Sheet{Path: path, Rev: rev(b), Headers: []string{}, Rows: [][]string{}}
	if len(recs) > 0 {
		sh.Headers = recs[0]
		for _, rec := range recs[1:] {
			row := make([]string, len(sh.Headers))
			copy(row, rec)
			sh.Rows = append(sh.Rows, row)
		}
	}
	return sh, nil
}

func encode(sh *Sheet) []byte {
	var buf bytes.Buffer
	w := csv.NewWriter(&buf)
	w.Write(sh.Headers)
	for _, r := range sh.Rows {
		w.Write(r)
	}
	w.Flush()
	return buf.Bytes()
}

func (s *Store) Create(path string, headers []string, rows [][]string) (*Sheet, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	abs, err := s.roots.Resolve(path)
	if err != nil {
		return nil, err
	}
	if _, err := os.Stat(abs); err == nil {
		return nil, fmt.Errorf("%s already exists", path)
	}
	if len(headers) == 0 {
		return nil, errors.New("a sheet needs at least one column header")
	}
	sh := &Sheet{Path: path, Headers: headers}
	for _, r := range rows {
		row := make([]string, len(headers))
		copy(row, r)
		sh.Rows = append(sh.Rows, row)
	}
	b := encode(sh)
	if err := files.WriteAtomic(abs, b); err != nil {
		return nil, err
	}
	sh.Rev = rev(b)
	return sh, nil
}

// Apply applies ops atomically. baseRev "" skips the stale check (agent writes
// read fresh state under the lock; user writes always pass their rev).
func (s *Store) Apply(path, baseRev string, ops []Op) (*Sheet, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	abs, err := s.roots.Resolve(path)
	if err != nil {
		return nil, err
	}
	b, err := os.ReadFile(abs)
	if err != nil {
		return nil, err
	}
	sh, err := parse(path, b)
	if err != nil {
		return nil, err
	}
	if baseRev != "" && baseRev != sh.Rev {
		return sh, ErrStale
	}
	for _, op := range ops {
		if err := apply(sh, op); err != nil {
			return nil, err
		}
	}
	nb := encode(sh)
	if err := files.WriteAtomic(abs, nb); err != nil {
		return nil, err
	}
	sh.Rev = rev(nb)
	return sh, nil
}

func apply(sh *Sheet, op Op) error {
	ncol := len(sh.Headers)
	inRow := func(i int) error {
		if i < 0 || i >= len(sh.Rows) {
			return fmt.Errorf("row %d out of range (0..%d)", i, len(sh.Rows)-1)
		}
		return nil
	}
	inCol := func(i int) error {
		if i < 0 || i >= ncol {
			return fmt.Errorf("column %d out of range (0..%d)", i, ncol-1)
		}
		return nil
	}
	switch op.Op {
	case "set":
		if err := inRow(op.Row); err != nil {
			return err
		}
		if err := inCol(op.Col); err != nil {
			return err
		}
		sh.Rows[op.Row][op.Col] = op.Value
	case "addRow":
		row := make([]string, ncol)
		copy(row, op.Cells)
		if op.At != nil && *op.At >= 0 && *op.At <= len(sh.Rows) {
			i := *op.At
			sh.Rows = append(sh.Rows[:i], append([][]string{row}, sh.Rows[i:]...)...)
		} else {
			sh.Rows = append(sh.Rows, row)
		}
	case "deleteRow":
		if err := inRow(op.Row); err != nil {
			return err
		}
		sh.Rows = append(sh.Rows[:op.Row], sh.Rows[op.Row+1:]...)
	case "addCol":
		sh.Headers = append(sh.Headers, op.Value)
		for i := range sh.Rows {
			sh.Rows[i] = append(sh.Rows[i], "")
		}
	case "renameCol":
		if err := inCol(op.Col); err != nil {
			return err
		}
		sh.Headers[op.Col] = op.Value
	case "deleteCol":
		if err := inCol(op.Col); err != nil {
			return err
		}
		if ncol == 1 {
			return errors.New("can't delete the last column")
		}
		sh.Headers = append(sh.Headers[:op.Col], sh.Headers[op.Col+1:]...)
		for i := range sh.Rows {
			sh.Rows[i] = append(sh.Rows[i][:op.Col], sh.Rows[i][op.Col+1:]...)
		}
	default:
		return fmt.Errorf("unknown op %q", op.Op)
	}
	return nil
}

// View is per-pane display state: sort and filter. Never touches the file.
type View struct {
	SortCol   int    `json:"sortCol"` // -1 = none
	SortDesc  bool   `json:"sortDesc,omitempty"`
	Filter    string `json:"filter,omitempty"`    // substring match, any column
	FilterCol int    `json:"filterCol,omitempty"` // -1 = any
}
