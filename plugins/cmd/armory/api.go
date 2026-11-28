// Package main implements the armory plugin: a World of Warcraft character
// from Blizzard's profile API and Raider.IO.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"image"
	"io"
	"math"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/maehld/paperboy/plugins/internal/imagelimit"
)

const (
	defaultBase      = "https://raider.io"
	defaultOAuthBase = "https://oauth.battle.net"
	requestTimeout   = 20 * time.Second
	maxBody          = 4 << 20
	retryAfterError  = time.Minute
)

// name is one character from the configuration, "Name@realm" or a bare name
// without a realm (a character of a ruleset without realms).
type name struct {
	char  string
	realm string
}

// parseName reads "Name@realm" or "Name".
func parseName(s string) (name, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return name{}, errors.New("empty character name")
	}
	c, realm, hasRealm := strings.Cut(s, "@")
	c, realm = strings.TrimSpace(c), strings.TrimSpace(realm)
	if c == "" {
		return name{}, fmt.Errorf("character %q has no name", s)
	}
	if !hasRealm {
		realm = ""
	}
	return name{char: c, realm: realm}, nil
}

// String is the character as it appears in the configuration.
func (n name) String() string {
	if n.realm == "" {
		return n.char
	}
	return n.char + "@" + n.realm
}

// display is the character's name as the API spells it.
func (c *character) display() string {
	if c.displayName != "" {
		return c.displayName
	}
	return c.name.char
}

// key identifies the character in the cache, independent of its spelling.
func (n name) key() string {
	return strings.ToLower(n.char + "@" + n.realm)
}

// slot is one equipped item.
type slot struct {
	name    string // the slot, localized by the API
	item    string // the item's name
	quality string // the rarity, localized by the API
	origin  string // where it comes from, e.g. "Raid · Heroic"
	level   int
}

// difficulty is one row of the raid progress.
type difficulty struct {
	key    string // normal, heroic, mythic
	killed int
	total  int
}

// character is everything the widgets show about one character.
type character struct {
	name   name
	region string

	level        int
	class        string
	spec         string
	guild        string
	displayName  string
	itemLevel    float64
	achievements int
	lastLogin    time.Time
	renderURL    string
	portrait     image.Image
	gear         []slot

	score       float64
	bestRun     string
	raid        string
	raidSummary string
	tiers       []difficulty

	fetched time.Time
	err     error
}

// armory is the plugin.
type armory struct {
	region   string
	base     string
	apiBase  string
	oauth    string
	clientID string
	secret   string
	ns       string
	refresh  time.Duration
	chars    []name
	lang     string

	mu      sync.Mutex
	data    map[string]*character
	cur     map[string]*character // the characters of the running request
	token   string
	expires time.Time
	client  *http.Client
}

// get reads a URL and decodes its answer into v.
func (a *armory) get(ctx context.Context, u string, hdr map[string]string, v any) error {
	ctx, cancel := context.WithTimeout(ctx, requestTimeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return err
	}
	for k, val := range hdr {
		req.Header.Set(k, val)
	}
	resp, err := a.http().Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxBody+1))
	if err != nil {
		return err
	}
	if resp.StatusCode == http.StatusNotFound {
		return errors.New("not found")
	}
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("HTTP %d", resp.StatusCode)
	}
	if len(body) > maxBody {
		return errors.New("answer too large")
	}
	if err := json.Unmarshal(body, v); err != nil {
		return fmt.Errorf("invalid answer: %w", err)
	}
	return nil
}

func (a *armory) http() *http.Client {
	if a.client != nil {
		return a.client
	}
	return http.DefaultClient
}

// picture downloads and decodes an image, such as the character render.
func (a *armory) picture(ctx context.Context, u string) (image.Image, error) {
	ctx, cancel := context.WithTimeout(ctx, requestTimeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return nil, err
	}
	resp, err := a.http().Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("HTTP %d", resp.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxBody+1))
	if err != nil {
		return nil, err
	}
	if len(body) > maxBody {
		return nil, errors.New("picture too large")
	}
	return imagelimit.Decode(body)
}

// accessToken asks for an application token and caches it until it nearly
// expires.
func (a *armory) accessToken(ctx context.Context) (string, error) {
	a.mu.Lock()
	tok, exp := a.token, a.expires
	a.mu.Unlock()
	if tok != "" && time.Now().Before(exp) {
		return tok, nil
	}

	ctx, cancel := context.WithTimeout(ctx, requestTimeout)
	defer cancel()
	form := url.Values{"grant_type": {"client_credentials"}}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost,
		strings.TrimRight(a.oauth, "/")+"/token", strings.NewReader(form.Encode()))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.SetBasicAuth(a.clientID, a.secret)
	resp, err := a.http().Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("token: HTTP %d", resp.StatusCode)
	}
	var out struct {
		AccessToken string `json:"access_token"`
		ExpiresIn   int    `json:"expires_in"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, maxBody)).Decode(&out); err != nil {
		return "", fmt.Errorf("token: %w", err)
	}
	if out.AccessToken == "" {
		return "", errors.New("token: no access token")
	}
	life := time.Duration(out.ExpiresIn) * time.Second
	if life <= 0 {
		life = time.Hour
	}
	a.mu.Lock()
	a.token, a.expires = out.AccessToken, time.Now().Add(life-time.Minute)
	a.mu.Unlock()
	return out.AccessToken, nil
}

// blizzardProfile is the part of the character profile the plugin uses.
type blizzardProfile struct {
	Name              string                 `json:"name"`
	Level             int                    `json:"level"`
	AchievementPoints int                    `json:"achievement_points"`
	EquippedItemLevel float64                `json:"equipped_item_level"`
	AverageItemLevel  float64                `json:"average_item_level"`
	LastLogin         int64                  `json:"last_login_timestamp"`
	Class             struct{ Name string }  `json:"character_class"`
	Spec              *struct{ Name string } `json:"active_spec"`
	Guild             *struct{ Name string } `json:"guild"`
}

// blizzardMedia holds the render pictures of a character.
type blizzardMedia struct {
	Assets []struct {
		Key   string `json:"key"`
		Value string `json:"value"`
	} `json:"assets"`
}

// blizzardEquipment is the equipped gear of a character.
type blizzardEquipment struct {
	Items []struct {
		Slot    struct{ Type, Name string } `json:"slot"`
		Name    string                      `json:"name"`
		Quality struct{ Type, Name string } `json:"quality"`
		Level   struct {
			Value   int    `json:"value"`
			Display string `json:"display_string"`
		} `json:"level"`
		Context json.RawMessage `json:"context"`
	} `json:"equipped_items"`
}

// tierProgress is the raid progress of one tier.
type tierProgress struct {
	Summary     string `json:"summary"`
	TotalBosses int    `json:"total_bosses"`
	Normal      int    `json:"normal_bosses_killed"`
	Heroic      int    `json:"heroic_bosses_killed"`
	Mythic      int    `json:"mythic_bosses_killed"`
}

// raiderIO is the part of the Raider.IO character profile the plugin uses.
type raiderIO struct {
	Name              string `json:"name"`
	Class             string `json:"class"`
	ActiveSpec        string `json:"active_spec_name"`
	AchievementPoints int    `json:"achievement_points"`
	Gear              struct {
		ItemLevel float64 `json:"item_level_equipped"`
	} `json:"gear"`
	Scores []struct {
		Season string `json:"season"`
		Scores struct {
			All float64 `json:"all"`
		} `json:"scores"`
	} `json:"mythic_plus_scores_by_season"`
	BestRuns []struct {
		Dungeon     string `json:"dungeon"`
		ShortName   string `json:"short_name"`
		MythicLevel int    `json:"mythic_level"`
	} `json:"mythic_plus_best_runs"`
	Progression map[string]tierProgress `json:"raid_progression"`
}

// load returns the character from the cache while its data is fresh.
func (a *armory) load(ctx context.Context, n name) *character {
	key := n.key()
	a.mu.Lock()
	c, ok := a.data[key]
	a.mu.Unlock()
	if ok {
		age := time.Since(c.fetched)
		if (c.err == nil && age < a.refresh) || (c.err != nil && age < retryAfterError) {
			return c
		}
	}
	c = a.fetch(ctx, n)
	a.mu.Lock()
	if a.data == nil {
		a.data = map[string]*character{}
	}
	a.data[key] = c
	a.mu.Unlock()
	return c
}

// fetch asks Raider.IO and, with credentials, Blizzard for one character. A
// failing source keeps the data of the other.
func (a *armory) fetch(ctx context.Context, n name) *character {
	c := &character{name: n, region: a.region, fetched: time.Now()}
	var errs []error
	if err := a.fetchRaiderIO(ctx, c); err != nil {
		errs = append(errs, err)
	}
	if a.clientID != "" && a.secret != "" {
		if err := a.fetchBlizzard(ctx, c); err != nil {
			errs = append(errs, err)
		}
	}
	if c.level == 0 && c.score == 0 && c.itemLevel == 0 {
		c.err = errors.Join(errs...)
		if c.err == nil {
			c.err = errors.New("no data")
		}
	}
	return c
}

// fetchRaiderIO reads the Mythic+ score, the best run and the raid progress,
// and fills the fields Blizzard would otherwise answer.
func (a *armory) fetchRaiderIO(ctx context.Context, c *character) error {
	q := url.Values{
		"region": {a.region},
		"realm":  {realmSlug(c.name.realm)},
		"name":   {c.name.char},
		"fields": {"mythic_plus_scores_by_season:current,mythic_plus_best_runs,raid_progression,gear"},
	}
	u := strings.TrimRight(a.base, "/") + "/api/v1/characters/profile?" + q.Encode()
	var r raiderIO
	if err := a.get(ctx, u, nil, &r); err != nil {
		return fmt.Errorf("raider.io: %w", err)
	}
	if r.Class != "" {
		c.class = r.Class
	}
	if r.Name != "" {
		c.displayName = r.Name
	}
	if r.ActiveSpec != "" {
		c.spec = r.ActiveSpec
	}
	if r.AchievementPoints > 0 {
		c.achievements = r.AchievementPoints
	}
	if r.Gear.ItemLevel > 0 {
		c.itemLevel = r.Gear.ItemLevel
	}
	if len(r.Scores) > 0 {
		c.score = math.Round(r.Scores[0].Scores.All*10) / 10
	}
	if len(r.BestRuns) > 0 {
		c.bestRun = fmt.Sprintf("%s +%d", r.BestRuns[0].Dungeon, r.BestRuns[0].MythicLevel)
	}
	a.raidProgress(c, r.Progression)
	return nil
}

// raidProgress takes the tier with the most bosses from the answer.
func (a *armory) raidProgress(c *character, p map[string]tierProgress) {
	slug := ""
	best := tierProgress{}
	for s, v := range p {
		if v.TotalBosses == 0 {
			continue
		}
		if slug == "" || v.TotalBosses > best.TotalBosses || (v.TotalBosses == best.TotalBosses && s > slug) {
			slug, best = s, v
		}
	}
	if slug == "" {
		return
	}
	c.raid = tierName(slug)
	c.raidSummary = best.Summary
	c.tiers = []difficulty{
		{key: "normal", killed: best.Normal, total: best.TotalBosses},
		{key: "heroic", killed: best.Heroic, total: best.TotalBosses},
		{key: "mythic", killed: best.Mythic, total: best.TotalBosses},
	}
}

// tierName turns a raid slug into its display name.
func tierName(slug string) string {
	words := strings.Split(strings.ReplaceAll(slug, "-", " "), " ")
	for i, w := range words {
		if w == "" {
			continue
		}
		if i > 0 {
			switch w {
			case "of", "the", "and", "to":
				continue
			}
		}
		words[i] = strings.ToUpper(w[:1]) + w[1:]
	}
	return strings.Join(words, " ")
}

// realmSlug is the realm in the form the APIs expect.
func realmSlug(realm string) string {
	s := strings.ToLower(strings.TrimSpace(realm))
	s = strings.ReplaceAll(s, "'", "")
	s = strings.ReplaceAll(s, " ", "-")
	return s
}

// fetchBlizzard reads the profile, the render and the equipment.
func (a *armory) fetchBlizzard(ctx context.Context, c *character) error {
	tok, err := a.accessToken(ctx)
	if err != nil {
		return fmt.Errorf("blizzard: %w", err)
	}
	hdr := map[string]string{"Authorization": "Bearer " + tok}
	ns := a.ns
	if ns == "" {
		ns = "profile-" + a.region
	}
	q := url.Values{"namespace": {ns}, "locale": {localeFor(a.lang)}}
	path := "/profile/wow/character/"
	if c.name.realm != "" {
		path += realmSlug(c.name.realm) + "/"
	}
	path += strings.ToLower(c.name.char)

	var p blizzardProfile
	if err := a.get(ctx, a.host()+path+"?"+q.Encode(), hdr, &p); err != nil {
		return fmt.Errorf("blizzard: %w", err)
	}
	c.level = p.Level
	c.class = p.Class.Name
	if p.Name != "" {
		c.displayName = p.Name
	}
	if p.Spec != nil {
		c.spec = p.Spec.Name
	}
	if p.Guild != nil {
		c.guild = p.Guild.Name
	}
	if p.AchievementPoints > 0 {
		c.achievements = p.AchievementPoints
	}
	if p.EquippedItemLevel > 0 {
		c.itemLevel = p.EquippedItemLevel
	} else if p.AverageItemLevel > 0 {
		c.itemLevel = p.AverageItemLevel
	}
	if p.LastLogin > 0 {
		c.lastLogin = time.UnixMilli(p.LastLogin)
	}

	var m blizzardMedia
	if err := a.get(ctx, a.host()+path+"/character-media?"+q.Encode(), hdr, &m); err == nil {
		for _, want := range []string{"main", "inset", "avatar"} {
			for _, asset := range m.Assets {
				if asset.Key == want && asset.Value != "" && c.renderURL == "" {
					c.renderURL = asset.Value
				}
			}
		}
	}
	if c.renderURL != "" {
		if img, err := a.picture(ctx, c.renderURL); err == nil {
			c.portrait = img
		}
	}

	var e blizzardEquipment
	if err := a.get(ctx, a.host()+path+"/equipment?"+q.Encode(), hdr, &e); err != nil {
		return fmt.Errorf("blizzard: %w", err)
	}
	for _, it := range e.Items {
		c.gear = append(c.gear, slot{
			name:    it.Slot.Name,
			item:    it.Name,
			quality: it.Quality.Name,
			origin:  originText(it.Context),
			level:   it.Level.Value,
		})
	}
	return nil
}

// host is the Blizzard API host of the region.
func (a *armory) host() string {
	if a.apiBase != "" {
		return strings.TrimRight(a.apiBase, "/")
	}
	return "https://" + a.region + ".api.blizzard.com"
}

// originText names where an item comes from. The API answers the context as a
// string ("raid-heroic") in older documents and as a number in newer ones.
func originText(raw json.RawMessage) string {
	if len(raw) == 0 {
		return ""
	}
	var s string
	if err := json.Unmarshal(raw, &s); err == nil {
		return contextSlug(s)
	}
	var n int
	if err := json.Unmarshal(raw, &n); err == nil {
		return contextID(n)
	}
	return ""
}

// contextSlug turns the item context into a short text, e.g. "Raid · Heroic".
func contextSlug(s string) string {
	s = strings.ToLower(s)
	switch {
	case s == "":
		return ""
	case strings.HasPrefix(s, "raid"):
		if _, mode, ok := strings.Cut(s, "-"); ok && mode != "" {
			return "Raid · " + capitalize(mode)
		}
		return "Raid"
	case strings.HasPrefix(s, "dungeon"):
		switch s {
		case "dungeon-heroic", "dungeon-level-up-3":
			return "Dungeon · Heroic"
		case "dungeon-mythic", "dungeon-level-up-4":
			return "Dungeon · Mythic"
		}
		return "Dungeon"
	case strings.HasPrefix(s, "challenge-mode"), strings.HasPrefix(s, "mythic-keystone"), strings.HasPrefix(s, "mplus"):
		return "Mythic+"
	case strings.HasPrefix(s, "pvp"):
		return "PvP"
	case strings.HasPrefix(s, "world-quest"):
		return "World quest"
	case s == "trade-skill":
		return "Crafted"
	case s == "vendor":
		return "Vendor"
	}
	return capitalize(s)
}

// contextID maps the numeric item context of newer documents. The numbers
// follow the enumeration of the game data; an unknown one stays empty.
func contextID(n int) string {
	switch n {
	case 1:
		return "Dungeon"
	case 2:
		return "Dungeon · Heroic"
	case 3:
		return "Dungeon · Mythic"
	case 4:
		return "Raid · Normal"
	case 5:
		return "Raid · Heroic"
	case 6:
		return "Raid · Mythic"
	case 7:
		return "Mythic+"
	}
	return ""
}

func capitalize(s string) string {
	if s == "" {
		return s
	}
	return strings.ToUpper(s[:1]) + s[1:]
}

// localeFor picks the API locale from Paperboy's language.
func localeFor(lang string) string {
	if lang == "de" {
		return "de_DE"
	}
	return "en_GB"
}
