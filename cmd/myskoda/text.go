package main

import (
	"fmt"
	"strings"

	"github.com/maehld/paperboy/pkg/plugin"
)

// Text fills the widgets of a character display.
func (m *myskoda) Text(_ *plugin.Request, w plugin.WidgetRequest) ([]string, error) {
	switch w.Widget {
	case "battery":
		return m.textBattery(w), nil
	case "charging":
		return m.textCharging(w), nil
	case "profiles":
		return m.textProfiles(w), nil
	case "location":
		return m.textLocation(w), nil
	case "climate":
		return m.textClimate(w), nil
	case "car":
		return m.textCar(w), nil
	}
	return nil, fmt.Errorf("%w %q for text displays", plugin.ErrUnknownWidget, w.Widget)
}

func (m *myskoda) textBattery(w plugin.WidgetRequest) []string {
	soc := "–"
	if v, ok := m.soc(); ok {
		soc = fmt.Sprintf("%d %%", v)
	}
	lines := []string{plugin.Columns(m.t("Akku", "Battery"), soc, w.Width)}
	state := m.chargeState()
	if p, ok := m.power(); ok {
		state += " " + m.num(p, 1) + " kW"
	}
	right := ""
	if v, ok := m.rangeKM(); ok && w.Arg().Bool("range", true) {
		right = fmt.Sprintf("%d km", v)
	}
	lines = append(lines, plugin.Columns(state, right, w.Width))
	if r := m.remaining(); r != "" {
		lines = append(lines, plugin.Columns(m.t("Voll", "Full"), r, w.Width))
	}
	if v, ok := m.targetSoC(); ok {
		lines = append(lines, plugin.Columns(m.t("Ziel", "Target"), fmt.Sprintf("%d %%", v), w.Width))
	}
	return lines
}

func (m *myskoda) textCharging(w plugin.WidgetRequest) []string {
	state := m.chargeState()
	if p, ok := m.power(); ok {
		state += " " + m.num(p, 1) + " kW"
	}
	lines := []string{plugin.Columns(state, m.remaining(), w.Width)}
	soc := ""
	if v, ok := m.soc(); ok {
		soc = fmt.Sprintf("%d %%", v)
		if t, ok := m.targetSoC(); ok {
			soc = fmt.Sprintf("%d→%d %%", v, t)
		}
	}
	if soc != "" {
		lines = append(lines, plugin.Columns(soc, "", w.Width))
	}
	if w.Arg().Bool("profile", true) {
		if p := m.currentProfile(); p != nil {
			lines = append(lines, plugin.Columns(p.Name, p.NextChargingTime, w.Width))
		}
	}
	return lines
}

func (m *myskoda) textProfiles(w plugin.WidgetRequest) []string {
	list := m.profileList(w.Arg().Int("max", 2))
	if len(list) == 0 {
		return []string{m.t("Keine Ladepläne", "No charging plans")}
	}
	current := m.currentProfile()
	var lines []string
	for _, p := range list {
		name := p.Name
		if current != nil && current.Name == p.Name {
			name = "* " + name
		}
		target := ""
		if p.Settings.TargetSoC != nil {
			target = fmt.Sprintf("%d %%", *p.Settings.TargetSoC)
		}
		lines = append(lines, plugin.Columns(name, target, w.Width))
		if times := m.profileTimes(p); times != "" && len(lines) < w.Height {
			lines = append(lines, cut(times, w.Width))
		}
	}
	return lines
}

func (m *myskoda) textLocation(w plugin.WidgetRequest) []string {
	p := m.cur.ParkingPosition
	if p == nil {
		return []string{m.t("Keine Position", "No position")}
	}
	if p.State == "IN_MOTION" {
		return []string{m.t("Unterwegs", "In motion")}
	}
	var lines []string
	if p.Address != "" {
		lines = wrapRunes(p.Address, w.Width, 3)
	}
	if gps := p.GPS; gps != nil && w.Arg().Bool("coords", true) {
		lines = append(lines, cut(m.num(gps.Latitude, 4)+", "+m.num(gps.Longitude, 4), w.Width))
	}
	if age := m.captured(p.Captured); age != "" {
		lines = append(lines, cut(age, w.Width))
	}
	if len(lines) == 0 {
		lines = []string{m.t("Geparkt", "Parked")}
	}
	return lines
}

func (m *myskoda) textClimate(w plugin.WidgetRequest) []string {
	if m.cur.AirConditioning == nil {
		return []string{m.t("Keine Klimadaten", "No climate data")}
	}
	c := m.cur.AirConditioning
	state := m.climateState()
	if temp := m.temperature(c.Target); temp != "" {
		state += " " + temp
	}
	lines := []string{cut(state, w.Width)}
	if t, ok := m.time(c.ReachAt); ok {
		lines = append(lines, plugin.Columns(m.t("erreicht", "reached"), t.Format("15:04"), w.Width))
	}
	if heat := m.windowHeating(c); heat != "" {
		lines = append(lines, cut(heat, w.Width))
	}
	if a := m.cur.AuxiliaryHeating; a != nil && a.State != nil && *a.State != "OFF" {
		lines = append(lines, cut(m.t("Standheizung: ", "Auxiliary: ")+m.auxState(*a.State), w.Width))
	}
	if v := m.cur.ActiveVentilation; v != nil && v.State != nil && *v.State != "OFF" {
		lines = append(lines, cut(m.t("Lüftung: ", "Ventilation: ")+m.climateToken(*v.State), w.Width))
	}
	return lines
}

func (m *myskoda) textCar(w plugin.WidgetRequest) []string {
	name := m.cur.Name
	if name == "" {
		name = m.cur.VIN
	}
	right := ""
	if v, ok := m.soc(); ok {
		right = fmt.Sprintf("%d %%", v)
	}
	lines := []string{plugin.Columns(name, right, w.Width)}
	var detail []string
	if v, ok := m.rangeKM(); ok {
		detail = append(detail, fmt.Sprintf("%d km", v))
	}
	detail = append(detail, m.chargeState())
	lines = append(lines, plugin.Columns(strings.Join(detail, " "), m.lockState(), w.Width))
	if o := m.cur.Odometer; o != nil && o.KM != nil {
		lines = append(lines, plugin.Columns(m.t("km", "Odometer"), m.thousands(*o.KM), w.Width))
	}
	return lines
}

// cut shortens s to width runes with an ellipsis.
func cut(s string, width int) string {
	r := []rune(s)
	if len(r) <= width {
		return s
	}
	if width <= 1 {
		return string(r[:max(width, 0)])
	}
	return string(r[:width-1]) + "…"
}

// wrapRunes breaks s into lines of at most width runes; a word that does not
// fit alone is split.
func wrapRunes(s string, width, maxLines int) []string {
	width = max(width, 1)
	var lines []string
	line := ""
	for _, word := range strings.Fields(s) {
		r := []rune(word)
		for len(r) > width {
			if line != "" {
				lines = append(lines, line)
				line = ""
			}
			lines = append(lines, string(r[:width]))
			r = r[width:]
		}
		word = string(r)
		if word == "" {
			continue
		}
		if line == "" {
			line = word
			continue
		}
		if len([]rune(line))+1+len([]rune(word)) > width {
			lines = append(lines, line)
			line = word
			continue
		}
		line += " " + word
	}
	if line != "" {
		lines = append(lines, line)
	}
	if maxLines > 0 && len(lines) > maxLines {
		lines = lines[:maxLines]
		lines[maxLines-1] = cut(lines[maxLines-1]+" …", width)
	}
	return lines
}
