package data

import (
	"errors"
	"path/filepath"
	"testing"
	"time"
)

func newDoc(t *testing.T, now *time.Time) *Document {
	t.Helper()
	d, err := New(filepath.Join(t.TempDir(), "data.json"), func() time.Time { return *now })
	if err != nil {
		t.Fatal(err)
	}
	return d
}

func TestPutAndGet(t *testing.T) {
	now := time.Date(2026, 10, 21, 9, 0, 0, 0, time.UTC)
	d := newDoc(t, &now)

	if err := d.Put("solar/current", map[string]any{"pv_power": 4210.0}, time.Time{}); err != nil {
		t.Fatal(err)
	}
	v, ok := d.Get("solar/current/pv_power")
	if !ok || v != 4210.0 {
		t.Fatalf("got %v, %t", v, ok)
	}
	if _, ok := d.Get("solar/history"); ok {
		t.Fatal("missing path must not be found")
	}

	// null is a valid value and stays distinguishable from a missing path.
	if err := d.Put("solar/current/grid", nil, time.Time{}); err != nil {
		t.Fatal(err)
	}
	if v, ok := d.Get("solar/current/grid"); !ok || v != nil {
		t.Fatalf("null must be present: %v, %t", v, ok)
	}

	// Replacing the root keeps only the new tree.
	if err := d.Put("", map[string]any{"wetter": map[string]any{"text": "Sonnig"}}, time.Time{}); err != nil {
		t.Fatal(err)
	}
	if _, ok := d.Get("solar"); ok {
		t.Fatal("root replace must drop the old tree")
	}
	if v, _ := d.Get("wetter/text"); v != "Sonnig" {
		t.Fatalf("root replace lost data: %v", v)
	}

	// The root must be an object; a scalar root is rejected instead of
	// producing a document that cannot be addressed by path.
	if err := d.Put("", 5.0, time.Time{}); !errors.Is(err, ErrInvalid) {
		t.Fatalf("scalar root: %v", err)
	}
}

func TestConflictThroughScalar(t *testing.T) {
	now := time.Date(2026, 10, 21, 9, 0, 0, 0, time.UTC)
	d := newDoc(t, &now)

	if err := d.Put("solar/current", 5.0, time.Time{}); err != nil {
		t.Fatal(err)
	}
	if err := d.Put("solar/current/latest", 1.0, time.Time{}); !errors.Is(err, ErrConflict) {
		t.Fatalf("expected conflict, got %v", err)
	}
	// A sibling path still works.
	if err := d.Put("solar/history", []any{1.0}, time.Time{}); err != nil {
		t.Fatal(err)
	}
}

func TestInvalidPaths(t *testing.T) {
	now := time.Date(2026, 10, 21, 9, 0, 0, 0, time.UTC)
	d := newDoc(t, &now)
	for _, path := range []string{"Solar", "solar/Ä", "solar//daily", "solar/daily!", "a b"} {
		if err := d.Put(path, 1.0, time.Time{}); !errors.Is(err, ErrInvalid) {
			t.Fatalf("%q: expected invalid path, got %v", path, err)
		}
	}
	for _, path := range []string{"solar", "solar/daily", "a_b-c/0"} {
		fresh := newDoc(t, &now)
		if err := fresh.Put(path, 1.0, time.Time{}); err != nil {
			t.Fatalf("%q: %v", path, err)
		}
	}
}

func TestAgeFollowsWritesAroundPath(t *testing.T) {
	now := time.Date(2026, 10, 21, 9, 0, 0, 0, time.UTC)
	d := newDoc(t, &now)

	if err := d.Put("solar", map[string]any{"current": 1.0}, time.Time{}); err != nil {
		t.Fatal(err)
	}
	if age, ok := d.Age("solar/current"); !ok || age != 0 {
		t.Fatalf("new write: age %v, %t", age, ok)
	}

	now = now.Add(10 * time.Minute)
	if age, _ := d.Age("solar"); age != 10*time.Minute {
		t.Fatalf("parent age: %v", age)
	}
	if _, ok := d.Age("wetter"); ok {
		t.Fatal("unrelated path must not be dated")
	}

	// A write below the path refreshes it as well.
	if err := d.Put("solar/daily", []any{1.0}, time.Time{}); err != nil {
		t.Fatal(err)
	}
	if age, _ := d.Age("solar"); age != 0 {
		t.Fatalf("child write must refresh the parent, age %v", age)
	}

	now = now.Add(3 * time.Minute)
	if err := d.Put("wetter", map[string]any{}, time.Time{}); err != nil {
		t.Fatal(err)
	}
	if age, _ := d.Age("solar"); age != 3*time.Minute {
		t.Fatalf("unrelated write must not refresh, age %v", age)
	}
}

func TestReplaceDropsChildTimestamps(t *testing.T) {
	now := time.Date(2026, 10, 21, 9, 0, 0, 0, time.UTC)
	d := newDoc(t, &now)
	if err := d.Put("solar/daily", []any{1.0}, time.Time{}); err != nil {
		t.Fatal(err)
	}
	if err := d.Put("solar", map[string]any{}, time.Time{}); err != nil {
		t.Fatal(err)
	}
	d.mu.Lock()
	_, child := d.writes["solar/daily"]
	_, parent := d.writes["solar"]
	d.mu.Unlock()
	if child || !parent {
		t.Fatalf("timestamps after replace: child %t, parent %t", child, parent)
	}
}

func TestSupplierTime(t *testing.T) {
	now := time.Date(2026, 10, 21, 9, 0, 0, 0, time.UTC)
	d := newDoc(t, &now)

	if err := d.Put("solar", 1.0, now.Add(-2*time.Hour)); err != nil {
		t.Fatal(err)
	}
	if age, _ := d.Age("solar"); age != 2*time.Hour {
		t.Fatalf("late delivery must stay old, age %v", age)
	}

	// A time in the future cannot keep data fresh.
	if err := d.Put("solar", 2.0, now.Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	if age, _ := d.Age("solar"); age != 0 {
		t.Fatalf("future time must be capped, age %v", age)
	}
}

func TestDelete(t *testing.T) {
	now := time.Date(2026, 10, 21, 9, 0, 0, 0, time.UTC)
	d := newDoc(t, &now)
	if err := d.Put("solar/daily/points", []any{1.0}, time.Time{}); err != nil {
		t.Fatal(err)
	}
	if err := d.Put("solar/current", 1.0, time.Time{}); err != nil {
		t.Fatal(err)
	}
	if err := d.Delete("solar/daily"); err != nil {
		t.Fatal(err)
	}
	if _, ok := d.Get("solar/daily"); ok {
		t.Fatal("subtree still present")
	}
	if v, ok := d.Get("solar/current"); !ok || v != 1.0 {
		t.Fatalf("sibling lost: %v, %t", v, ok)
	}
	if _, ok := d.Age("solar/daily"); ok {
		t.Fatal("timestamps of the deleted subtree must be gone")
	}
	if err := d.Delete("solar/daily"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("second delete: %v", err)
	}
	if err := d.Delete(""); !errors.Is(err, ErrInvalid) {
		t.Fatalf("root delete: %v", err)
	}

	// Deleting the last child prunes now empty intermediate objects.
	if err := d.Delete("solar/current"); err != nil {
		t.Fatal(err)
	}
	if _, ok := d.Get("solar"); ok {
		t.Fatal("empty parent must be pruned")
	}
}

func TestPersistence(t *testing.T) {
	now := time.Date(2026, 10, 21, 9, 0, 0, 0, time.UTC)
	path := filepath.Join(t.TempDir(), "data.json")
	d, err := New(path, func() time.Time { return now })
	if err != nil {
		t.Fatal(err)
	}
	if err := d.Put("solar/current", map[string]any{"pv_power": 4210.0}, time.Time{}); err != nil {
		t.Fatal(err)
	}

	// A restart keeps data and timestamps: stale detection must survive it.
	later := now.Add(20 * time.Minute)
	d2, err := New(path, func() time.Time { return later })
	if err != nil {
		t.Fatal(err)
	}
	if v, ok := d2.Get("solar/current/pv_power"); !ok || v != 4210.0 {
		t.Fatalf("data lost: %v, %t", v, ok)
	}
	if age, ok := d2.Age("solar/current"); !ok || age != 20*time.Minute {
		t.Fatalf("age after restart: %v, %t", age, ok)
	}
}

func TestGetReturnsCopy(t *testing.T) {
	now := time.Date(2026, 10, 21, 9, 0, 0, 0, time.UTC)
	d := newDoc(t, &now)
	if err := d.Put("solar", map[string]any{"current": 1.0}, time.Time{}); err != nil {
		t.Fatal(err)
	}
	v, _ := d.Get("solar")
	v.(map[string]any)["current"] = 99.0
	if again, _ := d.Get("solar/current"); again != 1.0 {
		t.Fatalf("document changed through a returned map: %v", again)
	}
}
