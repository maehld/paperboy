package paperboy

import (
	"strings"
	"testing"
	"time"
)

func TestVersionText(t *testing.T) {
	loc := time.FixedZone("CET", 2*60*60)
	cases := []struct {
		name string
		v    buildVersion
		want string
	}{
		{"local build", buildVersion{Version: "(devel)"}, "dev"},
		{"no build info", buildVersion{}, "dev"},
		{"release", buildVersion{Version: "v1.2.3", Revision: "0123456789abcdef", At: "2026-10-30T20:00:00Z"},
			"v1.2.3 · 012345678 · 2026-10-30 22:00"},
		{"dirty worktree", buildVersion{Revision: "0123456789abcdef", Modified: true}, "dev · 012345678+dirty"},
		{"pseudo version", buildVersion{Version: "v0.0.0-20261030223002-0bb9c234b652+dirty",
			Revision: "0bb9c234b652aa", At: "2026-10-30T20:00:00Z", Modified: true},
			"dev · 0bb9c234b+dirty · 2026-10-30 22:00"},
		{"tag with suffix", buildVersion{Version: "v1.2.3-rc1"}, "v1.2.3-rc1"},
		{"dirty without revision", buildVersion{Modified: true}, "dev · dirty"},
		{"unparsable date is dropped", buildVersion{Revision: "abcdef", At: "yesterday"}, "dev · abcdef"},
	}
	for _, c := range cases {
		if got := versionText(c.v, loc); got != c.want {
			t.Errorf("%s: %q, want %q", c.name, got, c.want)
		}
	}
}

// TestBuildOverrides covers builds without VCS information, such as the
// container image, where the release data comes from -X.
func TestBuildOverrides(t *testing.T) {
	tag, commit, at := buildVersionTag, buildCommit, buildTime
	defer func() { buildVersionTag, buildCommit, buildTime = tag, commit, at }()
	buildVersionTag, buildCommit, buildTime = "v1.2.3", "abcdef0123456789-dirty", "2026-10-30T20:00:00Z"

	v := applyBuildOverrides(buildVersion{Version: "(devel)", Revision: "stale", At: "old"})
	if v.Version != "v1.2.3" || v.Revision != "abcdef0123456789" || !v.Modified || v.At != "2026-10-30T20:00:00Z" {
		t.Fatalf("overrides: %+v", v)
	}
	buildVersionTag, buildCommit, buildTime = "", "", ""
	if v := applyBuildOverrides(buildVersion{Version: "(devel)"}); v.Version != "(devel)" {
		t.Fatalf("empty overrides must not change the build info: %+v", v)
	}
}

// TestDashboardShowsBuildInfo checks that the overview names the running build.
func TestDashboardShowsBuildInfo(t *testing.T) {
	s, _ := planServer(t, testPlan)
	body := htmlRequest(t, s, "/ui").Body.String()
	if want := versionText(build, s.cfg.Timezone); !strings.Contains(body, want) {
		t.Fatalf("build label %q missing: %s", want, body)
	}
	if !strings.Contains(body, `title="Go `) {
		t.Fatal("Go version missing from the build label")
	}
}
