package builtin

import (
	"fmt"
	"image"
	"image/color"
	"sort"
	"strings"

	"github.com/maehld/paperboy/pkg/plugin"
	"github.com/maehld/paperboy/pkg/render"
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

// statusLines is the text-display form of device-status: one device per line.
func statusLines(req *plugin.Request, a plugin.Values, width int) []string {
	devices := statusDevices(req, a)
	if len(devices) == 0 {
		return []string{noDeviceText(req)}
	}
	out := make([]string, 0, len(devices))
	for _, d := range devices {
		left := statusName(d)
		parts := statusParts(req, d, a)
		right := strings.Join(parts, " ")
		for len(parts) > 0 && runeLen(left)+1+runeLen(right) > width {
			parts = parts[:len(parts)-1]
			right = strings.Join(parts, " ")
		}
		out = append(out, plugin.Columns(left, right, width))
	}
	return out
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
		parts = append(parts, runtimeText(d.RuntimeS))
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

// runtimeText formats the estimate as "~12 d" or, below two days, "~18 h".
func runtimeText(seconds int64) string {
	switch {
	case seconds <= 0:
		return ""
	case seconds < 48*3600:
		hours := (seconds + 1800) / 3600
		if hours < 1 {
			hours = 1
		}
		return fmt.Sprintf("~%d h", hours)
	}
	return fmt.Sprintf("~%d d", (seconds+43200)/86400)
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

// battery draws the battery widget: an icon with the charge beside it.
func (Plugin) battery(req *plugin.Request, w plugin.WidgetRequest, img *image.Paletted) error {
	a := w.Arg()
	b := img.Rect.Inset(w.Style.Padding)
	if b.Empty() {
		return plugin.TooSmall(w)
	}
	dev := statusDevice(req, a.String("device", ""))
	label := a.String("label", "auto")
	text := batteryText(req, dev, label)
	size := min(float64(b.Dy())*0.65, req.TextPx(w)*1.1)
	iconH := min(float64(b.Dy())*0.72, size*1.15)
	iconW := int(iconH * 2)
	if iconW < 4 || int(iconH) < 3 {
		return plugin.TooSmall(w)
	}
	gap := max(2, int(size*0.3))
	textW := 0
	if text != "" {
		textW = render.Measure(text, size, false)
	}
	if iconW+gap+textW > b.Dx() {
		text, textW = "", 0
	}
	total := iconW + gap + textW
	if total > b.Dx() {
		iconW, iconH = b.Dx(), float64(b.Dx())/2
		total = iconW
	}
	if iconW < 4 || int(iconH) < 3 {
		return plugin.TooSmall(w)
	}
	x := b.Min.X
	al := align(a.String("align", "center"), render.Center)
	switch al {
	case render.Center:
		x = b.Min.X + (b.Dx()-total)/2
	case render.Right:
		x = b.Max.X - total
	}
	icon := image.Rect(x, b.Min.Y+(b.Dy()-int(iconH))/2, x+iconW, b.Min.Y+(b.Dy()+int(iconH))/2)
	var status *plugin.BatteryStatus
	if dev != nil {
		status = dev.Battery
	}
	drawBattery(img, icon, status)
	if text != "" {
		render.Text(img, text, icon.Max.X+gap, b.Min.Y+b.Dy()/2+int(size*0.36),
			render.Style{Size: size, Shrink: true}, b.Max.X-(icon.Max.X+gap), color.Black, render.Left)
	}
	return nil
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
		size := min(float64(b.Dy())*0.6, req.TextPx(w)*1.1)
		render.Text(img, noDeviceText(req), b.Min.X+b.Dx()/2, b.Min.Y+b.Dy()/2+int(size*0.36),
			render.Style{Size: size, Shrink: true}, b.Dx(), color.Black, render.Center)
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

// drawBattery draws an outline with a terminal, a charge fill and a charging
// bolt. status nil or without a percentage leaves the outline empty.
func drawBattery(img *image.Paletted, r image.Rectangle, status *plugin.BatteryStatus) {
	if r.Dx() < 4 || r.Dy() < 3 {
		return
	}
	line := max(1, r.Dy()/10)
	nub := max(2, r.Dx()/10)
	body := image.Rect(r.Min.X, r.Min.Y, r.Max.X-nub, r.Max.Y)
	render.HLine(img, body.Min.X, body.Min.Y, body.Max.X, line)
	render.HLine(img, body.Min.X, body.Max.Y-line, body.Max.X, line)
	render.VLine(img, body.Min.X, body.Min.Y, body.Max.Y, line)
	render.VLine(img, body.Max.X-line, body.Min.Y, body.Max.Y, line)
	render.FillSolid(img, image.Rect(body.Max.X, body.Min.Y+r.Dy()/3, r.Max.X, body.Max.Y-r.Dy()/3))
	inner := body.Inset(line + 1)
	if inner.Empty() {
		return
	}
	if status != nil && status.Percent != nil {
		percent := max(0, min(100, *status.Percent))
		fill := inner
		fill.Max.X = inner.Min.X + int(float64(inner.Dx())*percent/100)
		render.FillSolid(img, fill)
	}
	if status != nil && status.Charging && inner.Dx() > 4 && inner.Dy() > 4 {
		box := image.Rect(inner.Min.X+inner.Dx()/3, inner.Min.Y, inner.Min.X+inner.Dx()*2/3, inner.Max.Y)
		render.FillColor(img, box, render.White)
		cx := box.Min.X + box.Dx()/2
		render.Segment(img, cx+box.Dx()/4, box.Min.Y+box.Dy()/6, cx-box.Dx()/4, box.Min.Y+box.Dy()/2, line, false)
		render.Segment(img, cx-box.Dx()/4, box.Min.Y+box.Dy()/2, cx+box.Dx()/4, box.Max.Y-box.Dy()/6, line, false)
	}
}
