package builtin

import (
	"fmt"
	"image"
	"image/color"
	"sort"
	"strings"
	"time"

	"github.com/maehld/paperboy/sdk/plugin"
	"github.com/maehld/paperboy/sdk/render"
	"github.com/maehld/paperboy/server/internal/format"
)

// statusDevice finds the device a widget asks for: the id when set, otherwise
// the device the page is rendered for.
func statusDevice(req *plugin.Request, id string) *plugin.DeviceStatus {
	if req.ServerStatus == nil {
		return nil
	}
	if id == "" {
		return req.ServerStatus.CurrentDeviceStatus
	}
	if dev, ok := req.ServerStatus.SelectedDevicesStatus[id]; ok {
		return &dev
	}
	if cur := req.ServerStatus.CurrentDeviceStatus; cur != nil && cur.ID == id {
		return cur
	}
	return nil
}

// statusDevices resolves the devices argument of device-status. Without it the
// widget shows the current device. Unknown ids become a row with status
// "unknown", so a typo is visible.
func statusDevices(req *plugin.Request, a plugin.Values) []plugin.DeviceStatus {
	if req.ServerStatus == nil {
		return nil
	}
	ids := a.Strings("devices")
	if len(ids) == 0 {
		if dev := req.ServerStatus.CurrentDeviceStatus; dev != nil {
			return []plugin.DeviceStatus{*dev}
		}
		return nil
	}
	out := make([]plugin.DeviceStatus, 0, len(ids))
	seen := map[string]bool{}
	for _, id := range ids {
		if id == "" || seen[id] {
			continue
		}
		seen[id] = true
		if dev, ok := req.ServerStatus.SelectedDevicesStatus[id]; ok {
			out = append(out, dev)
			continue
		}
		if cur := req.ServerStatus.CurrentDeviceStatus; cur != nil && cur.ID == id {
			out = append(out, *cur)
			continue
		}
		out = append(out, plugin.DeviceStatus{ID: id, Status: "unknown", SeenS: -1})
	}
	sort.SliceStable(out, func(i, j int) bool {
		si, sj := staleness(out[i]), staleness(out[j])
		if si == sj {
			return out[i].ID < out[j].ID
		}
		return si > sj
	})
	return out
}

// staleness orders the rows of device-status: the device not seen for the
// longest time comes first, an unknown device before all others.
func staleness(d plugin.DeviceStatus) int64 {
	if d.SeenS < 0 {
		return 1<<62 - 1
	}
	return d.SeenS
}

// statusLineImage is one row of the image form.
func statusLineImage(req *plugin.Request, d plugin.DeviceStatus, a plugin.Values) (string, string) {
	return statusName(d), statusRight(req, d, a, " · ")
}

// statusName prefixes a stale device with a mark.
func statusName(d plugin.DeviceStatus) string {
	if mark := statusMark(d); mark != "" {
		return mark + " " + d.ID
	}
	return d.ID
}

func statusRight(req *plugin.Request, d plugin.DeviceStatus, a plugin.Values, sep string) string {
	return strings.Join(statusParts(req, d, a), sep)
}

func statusParts(req *plugin.Request, d plugin.DeviceStatus, a plugin.Values) []string {
	if d.Status == "unknown" {
		return []string{req.T("unbekannt", "unknown")}
	}
	var parts []string
	if a.Bool("battery", true) && d.Battery != nil {
		if v := batteryValue(req, d.Battery); v != "" {
			parts = append(parts, v)
		}
	}
	if a.Bool("runtime", true) && d.RuntimeS > 0 {
		parts = append(parts, format.Runtime(time.Duration(d.RuntimeS)*time.Second))
	}
	if a.Bool("seen", true) {
		if v := seenText(req, d.SeenS); v != "" {
			parts = append(parts, v)
		}
	}
	return parts
}

// statusMark keeps a stale row visible even when the right side is empty.
func statusMark(d plugin.DeviceStatus) string {
	switch d.Status {
	case "late":
		return "!"
	case "never", "unregistered", "unknown":
		return "?"
	}
	return ""
}

func noDeviceText(req *plugin.Request) string {
	return req.T("kein Gerät", "no device")
}

// cellNotice draws a short text centered in b, for a cell that has no data to
// show.
func cellNotice(req *plugin.Request, w plugin.WidgetRequest, b image.Rectangle, img *image.Paletted, text string) {
	size := min(float64(b.Dy())*0.6, req.TextPx(w)*1.1)
	render.Text(img, text, b.Min.X+b.Dx()/2, b.Min.Y+b.Dy()/2+int(size*0.36),
		render.Style{Size: size, Shrink: true}, b.Dx(), color.Black, render.Center)
}

// batteryValue is the charge of a device: its percentage, otherwise its
// voltage. German separates the number and the unit with a space.
func batteryValue(req *plugin.Request, b *plugin.BatteryStatus) string {
	if b == nil {
		return ""
	}
	if b.Percent != nil {
		return percentText(req, *b.Percent)
	}
	if b.Volts > 0 {
		return voltsText(req, b.Volts)
	}
	return ""
}

// batteryText is batteryValue for the battery and header widgets, including
// the label choice and a dash for a device without a reading.
func batteryText(req *plugin.Request, dev *plugin.DeviceStatus, label string) string {
	if label == "none" {
		return ""
	}
	if dev == nil || dev.Battery == nil {
		return "-"
	}
	b := dev.Battery
	switch label {
	case "percent":
		if b.Percent != nil {
			return percentText(req, *b.Percent)
		}
		return "-"
	case "volts":
		if b.Volts > 0 {
			return voltsText(req, b.Volts)
		}
		return "-"
	}
	if v := batteryValue(req, b); v != "" {
		return v
	}
	return "-"
}

func percentText(req *plugin.Request, percent float64) string {
	percent = max(0, min(100, percent))
	if req.Lang() == "de" {
		return fmt.Sprintf("%.0f %%", percent)
	}
	return fmt.Sprintf("%.0f%%", percent)
}

func voltsText(req *plugin.Request, volts float64) string {
	s := fmt.Sprintf("%.2f", volts)
	if req.Lang() == "de" {
		s = strings.ReplaceAll(s, ".", ",")
	}
	return s + " V"
}

// seenText compacts the time since the last contact: now, 5m, 2h or 3d.
func seenText(req *plugin.Request, seconds int64) string {
	switch {
	case seconds < 0:
		return ""
	case seconds < 60:
		return req.T("jetzt", "now")
	case seconds < 3600:
		return fmt.Sprintf("%dm", seconds/60)
	case seconds < 86400:
		return fmt.Sprintf("%dh", seconds/3600)
	}
	return fmt.Sprintf("%dd", seconds/86400)
}

func runeLen(s string) int { return len([]rune(s)) }

// battery draws the battery widget: a gauge with the charge beside or below it.
func (Plugin) battery(req *plugin.Request, w plugin.WidgetRequest, img *image.Paletted) error {
	a := w.Arg()
	b := img.Rect.Inset(w.Style.Padding)
	if b.Empty() {
		return plugin.TooSmall(w)
	}
	dev := statusDevice(req, a.String("device", ""))
	var status *plugin.BatteryStatus
	if dev != nil {
		status = dev.Battery
	}
	bl := layoutBattery(b, batteryText(req, dev, a.String("label", "auto")),
		align(a.String("align", "center"), render.Center))
	if bl.icon.Empty() && bl.text == "" {
		return plugin.TooSmall(w)
	}
	if !bl.icon.Empty() {
		drawBattery(img, bl.icon, status)
	}
	if bl.text != "" {
		render.Text(img, bl.text, bl.x, bl.baseline, render.Style{Size: bl.size, Shrink: true},
			bl.maxW, color.Black, bl.align)
	}
	return nil
}

// batteryBlock is the placement of the gauge and the value in one cell.
type batteryBlock struct {
	icon     image.Rectangle // empty when the cell has no room for the gauge
	text     string          // empty when the cell has no room for the value
	size     float64         // font size of the value
	x        int             // anchor of the value
	baseline int
	maxW     int
	align    render.Align
}

// layoutBattery places the gauge and the value in the padded cell b. The value
// sits beside the gauge in a wide cell and below it in a square or tall one.
// The gauge follows the cell height and never grows beyond it. The value takes
// its size from the gauge, shrinks before the gauge is dropped, and wins the
// cell when both do not fit (ADR 0121).
func layoutBattery(b image.Rectangle, text string, al render.Align) batteryBlock {
	if b.Empty() {
		return batteryBlock{}
	}
	w, h := b.Dx(), float64(b.Dy())
	gap := max(2, int(h/10))
	if text == "" {
		// The gauge alone keeps its normal size and does not stretch to the
		// cell width.
		iconH := h
		iconW := int(iconH * 2)
		if iconW > w {
			iconW, iconH = w, float64(w)/2
		}
		if iconW < 4 || int(iconH) < 3 {
			return batteryBlock{}
		}
		return batteryBlock{icon: gaugeRect(b, iconW, int(iconH), offsetX(b, iconW, al), (b.Dy()-int(iconH))/2)}
	}
	if w < 3*b.Dy() {
		// Square or tall cell: the gauge on top, the value below.
		iconH := (h - float64(gap)) * 3 / 5
		iconW := int(iconH * 2)
		if iconW > w {
			iconW, iconH = w, float64(w)/2
		}
		if iconW < 4 || int(iconH) < 3 {
			return valueOnly(b, text, al)
		}
		top := b.Min.Y + int(iconH) + gap
		valueH := float64(b.Max.Y - top)
		size := max(render.MinSize, valueH*0.85)
		return batteryBlock{
			icon:     gaugeRect(b, iconW, int(iconH), offsetX(b, iconW, al), 0),
			text:     text,
			size:     size,
			x:        anchorX(b, al),
			baseline: top + int(valueH/2+size*0.36),
			maxW:     w,
			align:    al,
		}
	}
	// Wide cell: the gauge on the left, the value on its right.
	iconH := h
	iconW := int(iconH * 2)
	size := iconH * 0.8
	avail := w - iconW - gap
	if avail < render.Measure(text, render.MinSize, false) {
		return valueOnly(b, text, al)
	}
	size = fitSize(text, size, avail)
	textW := render.Measure(text, size, false)
	total := iconW + gap + textW
	x := b.Min.X + (w-total)/2
	switch al {
	case render.Left:
		x = b.Min.X
	case render.Right:
		x = b.Max.X - total
	}
	return batteryBlock{
		icon:     gaugeRect(b, iconW, int(iconH), x-b.Min.X, 0),
		text:     text,
		size:     size,
		x:        x + iconW + gap,
		baseline: b.Min.Y + b.Dy()/2 + int(size*0.36),
		maxW:     b.Max.X - (x + iconW + gap),
		align:    render.Left,
	}
}

// valueOnly keeps the value and leaves the gauge out.
func valueOnly(b image.Rectangle, text string, al render.Align) batteryBlock {
	size := max(render.MinSize, float64(b.Dy())*0.8)
	return batteryBlock{
		text:     text,
		size:     size,
		x:        anchorX(b, al),
		baseline: b.Min.Y + b.Dy()/2 + int(size*0.36),
		maxW:     b.Dx(),
		align:    al,
	}
}

// gaugeRect places a gauge of iconW by iconH pixels in the padded cell b at
// the given offset from the cell's top left corner.
func gaugeRect(b image.Rectangle, iconW, iconH, dx, dy int) image.Rectangle {
	x, y := b.Min.X+dx, b.Min.Y+dy
	return image.Rect(x, y, x+iconW, y+iconH)
}

// offsetX is the left offset of a block of width w in b for an alignment.
func offsetX(b image.Rectangle, w int, al render.Align) int {
	switch al {
	case render.Center:
		return (b.Dx() - w) / 2
	case render.Right:
		return b.Dx() - w
	}
	return 0
}

// anchorX is the text anchor of an alignment in b.
func anchorX(b image.Rectangle, al render.Align) int {
	switch al {
	case render.Center:
		return b.Min.X + b.Dx()/2
	case render.Right:
		return b.Max.X
	}
	return b.Min.X
}

// capHeight is the height of a capital letter at size. The header sizes its
// battery gauge by it, so the gauge matches the date on every display.
func capHeight(size float64) float64 {
	m := render.Face(size, false).Metrics()
	if m.CapHeight <= 0 {
		return size * 0.7
	}
	return float64(m.CapHeight) / 64
}

// fitSize reduces size by single pixels until s fits maxW, but not below
// render.MinSize; render.Text shrinks by the same rule.
func fitSize(s string, size float64, maxW int) float64 {
	for size > render.MinSize && render.Measure(s, size, false) > maxW {
		size--
	}
	return size
}

// deviceStatus draws one row per device: the name on the left, battery,
// runtime and the last contact on the right.
func (Plugin) deviceStatus(req *plugin.Request, w plugin.WidgetRequest, img *image.Paletted) error {
	a := w.Arg()
	b := img.Rect.Inset(w.Style.Padding)
	if b.Empty() {
		return plugin.TooSmall(w)
	}
	devices := statusDevices(req, a)
	if len(devices) == 0 {
		cellNotice(req, w, b, img, noDeviceText(req))
		return nil
	}
	// Fewer rows than devices are possible in a short cell; the stalest rows
	// stay at the top.
	maxRows := max(1, b.Dy()/12)
	if len(devices) > maxRows {
		devices = devices[:maxRows]
	}
	rowH := b.Dy() / len(devices)
	size := min(req.TextPx(w), float64(rowH)*0.6)
	if size < render.MinSize {
		size = render.MinSize
	}
	for i, d := range devices {
		left, right := statusLineImage(req, d, a)
		baseline := b.Min.Y + i*rowH + rowH/2 + int(size*0.36)
		leftW := b.Dx() * 45 / 100
		render.Text(img, left, b.Min.X, baseline, render.Style{Size: size, Bold: true, Shrink: true},
			leftW, color.Black, render.Left)
		if right != "" {
			render.Text(img, right, b.Max.X, baseline, render.Style{Size: size, Shrink: true},
				b.Dx()-leftW, color.Black, render.Right)
		}
	}
	return nil
}

// batteryFrame returns the body of the gauge, the thickness of its outline and
// the width of its terminal. The warning level thickens the outline, so the
// level is visible without a number (ADR 0121).
func batteryFrame(r image.Rectangle, status *plugin.BatteryStatus) (body image.Rectangle, line, nub int) {
	line = max(1, r.Dy()/24)
	if status != nil {
		switch status.Level {
		case "warn":
			line++
		case "critical":
			line += 2
		}
	}
	line = min(line, max(1, r.Dy()/6))
	nub = max(2, r.Dx()/8)
	return image.Rect(r.Min.X, r.Min.Y, r.Max.X-nub, r.Max.Y), line, nub
}

// batteryPercent is the charge in percent, false when the device reports none.
func batteryPercent(status *plugin.BatteryStatus) (float64, bool) {
	if status == nil || status.Percent == nil {
		return 0, false
	}
	return max(0, min(100, *status.Percent)), true
}

// drawBattery draws the gauge of the battery widget and of the page header: an
// outline with a terminal, the charge in four or five segments, a bolt while
// charging and a slash for a missing reading (ADR 0121).
func drawBattery(img *image.Paletted, r image.Rectangle, status *plugin.BatteryStatus) {
	if r.Dx() < 4 || r.Dy() < 3 {
		return
	}
	body, line, _ := batteryFrame(r, status)
	render.HLine(img, body.Min.X, body.Min.Y, body.Max.X, line)
	render.HLine(img, body.Min.X, body.Max.Y-line, body.Max.X, line)
	render.VLine(img, body.Min.X, body.Min.Y, body.Max.Y, line)
	render.VLine(img, body.Max.X-line, body.Min.Y, body.Max.Y, line)
	nubH := r.Dy() / 2
	render.FillSolid(img, image.Rect(body.Max.X, r.Min.Y+(r.Dy()-nubH)/2, r.Max.X, r.Min.Y+(r.Dy()+nubH)/2))
	inner := body.Inset(line + 1)
	if inner.Empty() {
		return
	}
	percent, ok := batteryPercent(status)
	if ok {
		fillBattery(img, inner, line, percent)
	}
	switch {
	case !ok:
		// A missing reading keeps the outline and adds a slash, so it does not
		// look like an empty battery.
		drawBatterySlash(img, inner, line)
	case status.Charging:
		drawBatteryBolt(img, inner)
	}
}

// fillBattery draws the charge in five segments, each filled or empty. A step
// is wider than one pixel, so 84 % and 100 % differ at a glance.
func fillBattery(img *image.Paletted, inner image.Rectangle, line int, percent float64) {
	segs := 5
	if inner.Dx() < 20 {
		segs = 4
	}
	if inner.Dx() < 8 {
		segs = 1
	}
	gap := max(1, line)
	if inner.Dx() < segs*3 {
		gap = 0
	}
	filled := int(float64(segs)*percent/100 + 0.5)
	if filled == 0 && percent > 0 {
		filled = 1
	}
	segW := (inner.Dx() - (segs-1)*gap) / segs
	if segW < 1 {
		segs, filled, segW, gap = 1, 1, inner.Dx(), 0
	}
	for i := 0; i < filled; i++ {
		x0 := inner.Min.X + i*(segW+gap)
		x1 := x0 + segW
		if i == segs-1 {
			x1 = inner.Max.X
		}
		render.FillSolid(img, image.Rect(x0, inner.Min.Y, x1, inner.Max.Y))
	}
}

// drawBatterySlash marks a gauge without a reading: one diagonal line inside
// the outline.
func drawBatterySlash(img *image.Paletted, inner image.Rectangle, line int) {
	m := max(1, inner.Dx()/8)
	render.Segment(img, inner.Min.X+m, inner.Max.Y-m-1, inner.Max.X-m-1, inner.Min.Y+m, line, false)
}

// boltShape is the charging mark as a polygon in a unit box: the usual
// lightning shape, drawn over the fill instead of a band cut out of it.
var boltShape = [6][2]float64{
	{0.54, 0.03}, {0.12, 0.58}, {0.42, 0.58}, {0.36, 0.97}, {0.84, 0.42}, {0.54, 0.42},
}

// drawBatteryBolt knocks the charging mark out of the charge, white on the fill
// and black on the empty part, so the mark stays visible on both.
func drawBatteryBolt(img *image.Paletted, inner image.Rectangle) {
	bw := inner.Dx() * 6 / 10
	bh := inner.Dy()
	if bw < 3 || bh < 4 {
		return
	}
	x0 := inner.Min.X + (inner.Dx()-bw)/2
	pts := make([]image.Point, len(boltShape))
	for i, p := range boltShape {
		pts[i] = image.Point{X: x0 + int(p[0]*float64(bw)), Y: inner.Min.Y + int(p[1]*float64(bh))}
	}
	for y := inner.Min.Y; y < inner.Max.Y; y++ {
		yc := float64(y) + 0.5
		var xs []float64
		for i, p := range pts {
			q := pts[(i+1)%len(pts)]
			if p.Y == q.Y || yc < float64(min(p.Y, q.Y)) || yc >= float64(max(p.Y, q.Y)) {
				continue
			}
			xs = append(xs, float64(p.X)+(yc-float64(p.Y))/float64(q.Y-p.Y)*float64(q.X-p.X))
		}
		sort.Float64s(xs)
		for i := 0; i+1 < len(xs); i += 2 {
			x1, x2 := int(xs[i]+0.5), int(xs[i+1]+0.5)
			if x2 < x1+1 {
				x2 = x1 + 1
			}
			invertRect(img, image.Rect(max(x1, inner.Min.X), y, min(x2, inner.Max.X), y+1))
		}
	}
}

// invertRect swaps the pixels of r between black and white.
func invertRect(img *image.Paletted, r image.Rectangle) {
	r = r.Intersect(img.Rect)
	last := uint8(len(img.Palette) - 1)
	for y := r.Min.Y; y < r.Max.Y; y++ {
		for x := r.Min.X; x < r.Max.X; x++ {
			i := img.PixOffset(x, y)
			img.Pix[i] = last - img.Pix[i]
		}
	}
}
