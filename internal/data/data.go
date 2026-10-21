// Package data holds the shared data document that page templates render
// from (ADR 0024). Suppliers write branches of the document through the path
// API; the package keeps a write timestamp per path so that stale data can be
// detected without comparing content.
package data

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// Errors returned by the path API. The HTTP handlers map them to status codes.
var (
	ErrInvalid  = errors.New("invalid path")
	ErrConflict = errors.New("path conflict")
	ErrNotFound = errors.New("not found")
)

// Document is one JSON document plus the time of the last write per path.
// It is safe for concurrent use.
type Document struct {
	mu     sync.Mutex
	path   string
	now    func() time.Time
	root   map[string]any
	writes map[string]time.Time // path → last write; "" is the root
}

// stored is the on-disk form of a Document.
type stored struct {
	Data   map[string]any       `json:"data"`
	Writes map[string]time.Time `json:"writes,omitempty"`
}

// New loads the document from path, or starts empty if the file does not
// exist yet. now defaults to time.Now.
func New(path string, now func() time.Time) (*Document, error) {
	if now == nil {
		now = time.Now
	}
	d := &Document{path: path, now: now, root: map[string]any{}, writes: map[string]time.Time{}}
	raw, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return d, nil
	}
	if err != nil {
		return nil, err
	}
	var f stored
	if err := json.Unmarshal(raw, &f); err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	if f.Data != nil {
		d.root = f.Data
	}
	if f.Writes != nil {
		d.writes = f.Writes
	}
	return d, nil
}

// Put replaces the value at path. Missing intermediate objects are created; an
// intermediate that is not an object is a conflict. at is the time of the
// write reported by the supplier (zero = now); times in the future are capped
// at now.
func (d *Document) Put(path string, value any, at time.Time) error {
	segs, err := segments(path)
	if err != nil {
		return err
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	if len(segs) == 0 {
		root, ok := value.(map[string]any)
		if !ok {
			return fmt.Errorf("%w: the root document must be an object", ErrInvalid)
		}
		d.root = root
		d.writes = map[string]time.Time{"": d.stamp(at)}
		return d.save()
	}
	cur := d.root
	for i, seg := range segs[:len(segs)-1] {
		next, ok := cur[seg]
		if !ok {
			child := map[string]any{}
			cur[seg] = child
			cur = child
			continue
		}
		child, ok := next.(map[string]any)
		if !ok {
			return fmt.Errorf("%w: %q is not an object", ErrConflict, strings.Join(segs[:i+1], "/"))
		}
		cur = child
	}
	cur[segs[len(segs)-1]] = value
	d.mark(strings.Join(segs, "/"), at)
	return d.save()
}

// Get returns the value at path and whether it exists. A stored null is
// present and returns ok = true.
func (d *Document) Get(path string) (any, bool) {
	segs, err := segments(path)
	if err != nil {
		return nil, false
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	if len(segs) == 0 {
		return clone(d.root), true
	}
	var cur any = d.root
	for _, seg := range segs {
		m, ok := cur.(map[string]any)
		if !ok {
			return nil, false
		}
		cur, ok = m[seg]
		if !ok {
			return nil, false
		}
	}
	return clone(cur), true
}

// Delete removes the subtree at path together with its timestamps. The root
// cannot be deleted; it is replaced with Put.
func (d *Document) Delete(path string) error {
	segs, err := segments(path)
	if err != nil {
		return err
	}
	if len(segs) == 0 {
		return fmt.Errorf("%w: the root document cannot be deleted", ErrInvalid)
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	cur := d.root
	for i, seg := range segs[:len(segs)-1] {
		child, ok := cur[seg].(map[string]any)
		if !ok {
			return fmt.Errorf("%w: %s", ErrNotFound, strings.Join(segs[:i+1], "/"))
		}
		cur = child
	}
	last := segs[len(segs)-1]
	if _, ok := cur[last]; !ok {
		return fmt.Errorf("%w: %s", ErrNotFound, strings.Join(segs, "/"))
	}
	delete(cur, last)
	d.prune(segs)
	key := strings.Join(segs, "/")
	for k := range d.writes {
		if k == key || strings.HasPrefix(k, key+"/") {
			delete(d.writes, k)
		}
	}
	return d.save()
}

// prune removes objects that became empty through a delete.
func (d *Document) prune(segs []string) {
	for i := len(segs) - 1; i > 0; i-- {
		parent := d.root
		ok := true
		for _, seg := range segs[:i-1] {
			child, isMap := parent[seg].(map[string]any)
			if !isMap {
				ok = false
				break
			}
			parent = child
		}
		if !ok {
			return
		}
		child, isMap := parent[segs[i-1]].(map[string]any)
		if !isMap || len(child) > 0 {
			return
		}
		delete(parent, segs[i-1])
	}
}

// Age returns how long ago the newest write that affects path happened. A
// write on a parent or a child path counts as well, so replacing a whole
// branch also refreshes the paths below it.
func (d *Document) Age(path string) (time.Duration, bool) {
	segs, err := segments(path)
	if err != nil {
		return 0, false
	}
	key := strings.Join(segs, "/")
	d.mu.Lock()
	defer d.mu.Unlock()
	var newest time.Time
	for k, at := range d.writes {
		if related(k, key) && at.After(newest) {
			newest = at
		}
	}
	if newest.IsZero() {
		return 0, false
	}
	age := d.now().Sub(newest)
	if age < 0 {
		age = 0
	}
	return age, true
}

// related reports whether two paths are equal or one contains the other.
func related(a, b string) bool {
	if a == b || a == "" || b == "" {
		return true
	}
	return strings.HasPrefix(a, b+"/") || strings.HasPrefix(b, a+"/")
}

// mark records a write at path and drops the timestamps below it, because the
// subtree was replaced as a whole.
func (d *Document) mark(path string, at time.Time) {
	for k := range d.writes {
		if k == path || strings.HasPrefix(k, path+"/") {
			delete(d.writes, k)
		}
	}
	d.writes[path] = d.stamp(at)
}

// stamp caps future times at now so that a supplier cannot keep data fresh by
// reporting a time ahead of the clock.
func (d *Document) stamp(at time.Time) time.Time {
	now := d.now()
	if at.IsZero() || at.After(now) {
		return now
	}
	return at
}

func (d *Document) save() error {
	raw, err := json.MarshalIndent(stored{Data: d.root, Writes: d.writes}, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(d.path), 0o755); err != nil {
		return err
	}
	tmp := d.path + ".tmp"
	if err := os.WriteFile(tmp, raw, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, d.path)
}

func segments(path string) ([]string, error) {
	path = strings.Trim(path, "/")
	if path == "" {
		return nil, nil
	}
	segs := strings.Split(path, "/")
	for _, seg := range segs {
		if !validSegment(seg) {
			return nil, fmt.Errorf("%w: segment %q must consist of a-z, 0-9, _ and -", ErrInvalid, seg)
		}
	}
	return segs, nil
}

func validSegment(s string) bool {
	if s == "" {
		return false
	}
	for _, r := range s {
		if !(r >= 'a' && r <= 'z' || r >= '0' && r <= '9' || r == '_' || r == '-') {
			return false
		}
	}
	return true
}

// clone decouples returned values from the document; callers must not be able
// to change it through a returned map.
func clone(v any) any {
	if v == nil {
		return nil
	}
	raw, err := json.Marshal(v)
	if err != nil {
		return v
	}
	var out any
	if err := json.Unmarshal(raw, &out); err != nil {
		return v
	}
	return out
}
