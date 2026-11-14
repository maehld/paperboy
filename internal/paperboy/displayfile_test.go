package paperboy

import (
	"net/http"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

// writeDisplayFile writes displays/<name>.yaml into an instance directory
// (ADR 0071).
func writeDisplayFile(t *testing.T, dir, name, content string) {
	t.Helper()
	path := filepath.Join(dir, displaysDirName, name+".yaml")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

// TestDisplayFileRoundTrip verifies that a display type file survives parse and
// encode unchanged, that the encoder keeps only set fields, and that parsing is
// strict.
func TestDisplayFileRoundTrip(t *testing.T) {
	src := `kind: image
size: [800, 480]
gray_levels: 4
ppi: 125
min_refresh: 1m
power: mains
rotation: 90
`
	f, err := parseDisplayFile([]byte(src))
	if err != nil {
		t.Fatal(err)
	}
	data, err := encodeDisplayFile(&f)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "min_refresh: 1m\n") || !strings.Contains(string(data), "size: [800, 480]\n") ||
		!strings.Contains(string(data), "rotation: 90\n") {
		t.Fatalf("layout:\n%s", data)
	}
	again, err := parseDisplayFile(data)
	if err != nil {
		t.Fatalf("written display type:\n%s\n%v", data, err)
	}
	if !reflect.DeepEqual(f, again) {
		t.Fatalf("round trip changed the model:\n%+v\n%+v\n%s", f, again, data)
	}

	// A minimal file writes the kind and the size, and nothing else.
	minimal, err := encodeDisplayFile(&displayFile{Kind: "text", Size: [2]int{20, 4}, Charset: "utf8"})
	if err != nil {
		t.Fatal(err)
	}
	if want := "kind: text\nsize: [20, 4]\ncharset: utf8\n"; string(minimal) != want {
		t.Fatalf("minimal display type file:\n%s\nwant:\n%s", minimal, want)
	}

	// Unknown keys, a second document and an empty file are errors.
	for _, bad := range []string{"grey: 4\n", "size: [1, 1]\n---\nppi: 125\n", ""} {
		if _, err := parseDisplayFile([]byte(bad)); err == nil {
			t.Fatalf("%q parses as a display type", bad)
		}
	}
}

// TestDisplayFileValidation covers the checks the loader and the API share.
func TestDisplayFileValidation(t *testing.T) {
	image := displayFile{Kind: kindImage, Size: [2]int{800, 480}, PPI: 125}
	if err := image.check("e1001"); err != nil {
		t.Fatal(err)
	}
	p := image.profile()
	if err := validateProfile("e1001", &p); err != nil {
		t.Fatal(err)
	}
	if p.Power != powerAuto || p.Kind != kindImage || p.Levels != 2 || p.Width != 800 {
		t.Fatalf("image defaults: %+v", p)
	}
	text := displayFile{Kind: kindText, Size: [2]int{20, 4}}
	tp := text.profile()
	if err := validateProfile("lcd", &tp); err != nil || tp.Charset != "utf8" {
		t.Fatalf("text defaults: %+v %v", tp, err)
	}
	sixteen := displayFile{Kind: kindImage, Size: [2]int{1872, 1404}, GrayLevels: 16, PPI: 227}
	sp := sixteen.profile()
	if err := validateProfile("e1003", &sp); err != nil {
		t.Fatal(err)
	}
	if sp.Levels != 16 {
		t.Fatalf("16-level display type: %+v", sp)
	}

	for _, tc := range []struct {
		name string
		file displayFile
		want string
	}{
		{"name", displayFile{Size: [2]int{800, 480}, PPI: 125}, "only a-z"},
		{"kind", displayFile{Kind: "led", Size: [2]int{800, 480}, PPI: 125}, "unknown kind"},
		{"missing ppi", displayFile{Size: [2]int{800, 480}}, "ppi is required"},
		{"low ppi", displayFile{Size: [2]int{800, 480}, PPI: 19}, "ppi is required"},
		{"high ppi", displayFile{Size: [2]int{800, 480}, PPI: 1001}, "ppi is required"},
		{"charset on image", displayFile{Size: [2]int{800, 480}, PPI: 125, Charset: "ascii"}, "charset only applies"},
		{"text too large", displayFile{Kind: kindText, Size: [2]int{81, 4}}, "at most [80, 24]"},
		{"text charset", displayFile{Kind: kindText, Size: [2]int{20, 4}, Charset: "latin1"}, "expected utf8"},
		{"ppi on text", displayFile{Kind: kindText, Size: [2]int{20, 4}, PPI: 125}, "do not apply to kind text"},
		{"gray on text", displayFile{Kind: kindText, Size: [2]int{20, 4}, GrayLevels: 4}, "do not apply to kind text"},
		{"gray levels", displayFile{Size: [2]int{800, 480}, PPI: 125, GrayLevels: 2}, "gray_levels must be 1, 4 or 16"},
		{"min refresh", displayFile{Size: [2]int{800, 480}, PPI: 125, MinRefresh: Duration(500 * time.Millisecond)}, "at least 1s"},
		{"power", displayFile{Size: [2]int{800, 480}, PPI: 125, Power: "solar"}, "expected auto, battery or mains"},
		{"rotation", displayFile{Size: [2]int{480, 800}, PPI: 125, Rotation: 45}, "rotation must be 0, 90, 180 or 270"},
		{"rotation on text", displayFile{Kind: kindText, Size: [2]int{20, 4}, Rotation: 90}, "rotation does not apply to text displays"},
	} {
		name := "e1001"
		if tc.name == "name" {
			name = "Gross"
		}
		if err := tc.file.check(name); err != nil {
			if !strings.Contains(err.Error(), tc.want) {
				t.Errorf("%s: error %q does not mention %q", tc.name, err, tc.want)
			}
			continue
		}
		p := tc.file.profile()
		err := validateProfile(name, &p)
		if err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Errorf("%s: error %v, want %q", tc.name, err, tc.want)
		}
	}

	widthMM := 163
	if err := (displayFile{Size: [2]int{800, 480}, PPI: 125, WidthMM: &widthMM}).check("e1001"); err == nil ||
		!strings.Contains(err.Error(), "width_mm was replaced by ppi") {
		t.Fatalf("width_mm: %v", err)
	}
}

// TestDisplayAPI covers read, create, save, conflicts and validation of the
// display type API (ADR 0066, 0071).
func TestDisplayAPI(t *testing.T) {
	s := editorServer(t)
	dir := s.config().DisplaysDir

	if rec := request(t, s, http.MethodGet, "/api/displays/kueche", nil, nil); rec.Code != http.StatusNotFound {
		t.Fatalf("missing display type: %d", rec.Code)
	}
	model := &displayFile{Kind: kindImage, Size: [2]int{800, 480}, GrayLevels: 4, PPI: 125, MinRefresh: Duration(time.Minute)}
	var created editorDisplayType
	rec := request(t, s, http.MethodPut, "/api/displays/kueche", displaySave{Display: model}, &created)
	if rec.Code != http.StatusOK {
		t.Fatalf("create: %d %s", rec.Code, rec.Body)
	}
	if created.Hash == "" || created.Path != "displays/kueche.yaml" || created.Display == nil || created.Display.PPI != 125 {
		t.Fatalf("created: %+v", created)
	}
	data, err := os.ReadFile(filepath.Join(dir, "kueche.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "size: [800, 480]") || !strings.Contains(string(data), "min_refresh: 1m") {
		t.Fatalf("file:\n%s", data)
	}

	var read editorDisplayType
	if rec := request(t, s, http.MethodGet, "/api/displays/kueche", nil, &read); rec.Code != http.StatusOK ||
		read.Hash != created.Hash || read.Display == nil || read.Display.GrayLevels != 4 {
		t.Fatalf("read: %d %+v %s", rec.Code, read, rec.Body)
	}

	// A second save keeps a backup under state/editor; a stale hash is a
	// conflict afterwards.
	edited := *read.Display
	edited.Power = powerMains
	if rec := request(t, s, http.MethodPut, "/api/displays/kueche", displaySave{Hash: read.Hash, Display: &edited}, nil); rec.Code != http.StatusOK {
		t.Fatalf("save: %d %s", rec.Code, rec.Body)
	}
	if saved, err := os.ReadFile(filepath.Join(dir, "kueche.yaml")); err != nil || !strings.Contains(string(saved), "power: mains") {
		t.Fatalf("saved file: %s %v", saved, err)
	}
	backups, err := filepath.Glob(filepath.Join(s.config().Dir, "state", "editor", "displays", "kueche", "*.yaml"))
	if err != nil || len(backups) != 1 {
		t.Fatalf("backups: %v %v", backups, err)
	}
	if rec := request(t, s, http.MethodPut, "/api/displays/kueche", displaySave{Hash: read.Hash, Display: &edited}, nil); rec.Code != http.StatusConflict {
		t.Fatalf("stale hash: %d %s", rec.Code, rec.Body)
	}

	// The API rejects what a reload would reject, plus unknown fields and an
	// invalid name.
	bad := edited
	bad.PPI = 0
	if rec := request(t, s, http.MethodPut, "/api/displays/kueche", displaySave{Display: &bad}, nil); rec.Code != http.StatusBadRequest {
		t.Fatalf("missing ppi: %d %s", rec.Code, rec.Body)
	}
	bad = edited
	bad.Kind = kindText
	if rec := request(t, s, http.MethodPut, "/api/displays/kueche", displaySave{Display: &bad}, nil); rec.Code != http.StatusBadRequest {
		t.Fatalf("ppi on a text display: %d %s", rec.Code, rec.Body)
	}
	widthMM := 163
	bad = edited
	bad.WidthMM = &widthMM
	if rec := request(t, s, http.MethodPut, "/api/displays/kueche", displaySave{Display: &bad}, nil); rec.Code != http.StatusBadRequest ||
		!strings.Contains(rec.Body.String(), "width_mm") {
		t.Fatalf("width_mm: %d %s", rec.Code, rec.Body)
	}
	if rec := request(t, s, http.MethodPut, "/api/displays/kueche", `{"display":{"size":[800,480],"ppi":125},"nope":1}`, nil); rec.Code != http.StatusBadRequest {
		t.Fatalf("unknown field: %d", rec.Code)
	}
	if rec := request(t, s, http.MethodPut, "/api/displays/Not%20An%20ID", displaySave{Display: model}, nil); rec.Code != http.StatusBadRequest {
		t.Fatalf("bad name: %d", rec.Code)
	}

	var list struct{ Displays []editorDisplayFile }
	if rec := request(t, s, http.MethodGet, "/api/displays", nil, &list); rec.Code != http.StatusOK || len(list.Displays) != 1 ||
		list.Displays[0].Name != "kueche" || list.Displays[0].PPI != 125 {
		t.Fatalf("list: %d %+v %s", rec.Code, list, rec.Body)
	}
}
