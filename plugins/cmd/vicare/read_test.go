package main

import (
	"encoding/json"
	"math"
	"testing"
	"time"
)

// featuresJSON is one features answer of a boiler with two circuits.
const featuresJSON = `{"data":[
 {"feature":"heating.sensors.temperature.outside","deviceId":"0","timestamp":"2026-11-28T10:00:00Z",
  "properties":{"value":{"type":"number","unit":"celsius","value":4.2}}},
 {"feature":"heating.boiler.sensors.temperature.main","deviceId":"0","timestamp":"2026-11-28T09:59:00Z",
  "properties":{"value":{"type":"number","unit":"celsius","value":52.0}}},
 {"feature":"heating.sensors.temperature.return","deviceId":"0","timestamp":"2026-11-28T09:59:00Z",
  "properties":{"value":{"type":"number","unit":"celsius","value":38.0}}},
 {"feature":"heating.dhw.sensors.temperature.hotWaterStorage","deviceId":"0","timestamp":"2026-11-28T09:58:00Z",
  "properties":{"value":{"type":"number","unit":"celsius","value":48.0}}},
 {"feature":"heating.dhw.temperature.main","deviceId":"0",
  "properties":{"value":{"type":"number","unit":"celsius","value":50.0}}},
 {"feature":"heating.dhw.charging","deviceId":"0","properties":{"active":{"type":"boolean","value":true}}},
 {"feature":"heating.dhw.operating.modes.active","deviceId":"0","properties":{"value":{"type":"string","value":"balanced"}}},
 {"feature":"heating.dhw.oneTimeCharge","deviceId":"0","properties":{"active":{"type":"boolean","value":false}}},
 {"feature":"heating.burners.0","deviceId":"0","properties":{"active":{"type":"boolean","value":true}}},
 {"feature":"heating.burners.0.modulation","deviceId":"0","properties":{"value":{"type":"number","unit":"percent","value":42}}},
 {"feature":"heating.burners.0.statistics","deviceId":"0",
  "properties":{"hours":{"type":"number","unit":"hour","value":8518},"starts":{"type":"number","value":43345}}},
 {"feature":"heating.circuits.0.name","deviceId":"0","properties":{"name":{"type":"string","value":"Wohnzimmer"}}},
 {"feature":"heating.circuits.0.sensors.temperature.room","deviceId":"0",
  "properties":{"value":{"type":"number","unit":"celsius","value":21.5}}},
 {"feature":"heating.circuits.0.sensors.temperature.supply","deviceId":"0",
  "properties":{"value":{"type":"number","unit":"celsius","value":41.8}}},
 {"feature":"heating.circuits.0.temperature","deviceId":"0",
  "properties":{"value":{"type":"number","unit":"celsius","value":45.2}}},
 {"feature":"heating.circuits.0.operating.programs.active","deviceId":"0",
  "properties":{"value":{"type":"string","value":"normal"}}},
 {"feature":"heating.circuits.0.operating.modes.active","deviceId":"0",
  "properties":{"value":{"type":"string","value":"dhwAndHeating"}}},
 {"feature":"heating.circuits.0.heating.curve","deviceId":"0",
  "properties":{"slope":{"type":"number","value":0.6},"shift":{"type":"number","value":4}}},
 {"feature":"heating.circuits.0.temperature.levels","deviceId":"0",
  "properties":{"min":{"type":"number","value":20},"max":{"type":"number","value":55}}},
 {"feature":"heating.circuits.1.operating.programs.active","deviceId":"0",
  "properties":{"value":{"type":"string","value":"reduced"}}},
 {"feature":"heating.compressors.0","deviceId":"0",
  "properties":{"active":{"type":"boolean","value":false},"phase":{"type":"string","value":"off"}}},
 {"feature":"heating.compressors.0.statistics","deviceId":"0",
  "properties":{"hours":{"type":"number","value":13651.9},"starts":{"type":"number","value":4012}}},
 {"feature":"device.messages.errors.raw","deviceId":"0",
  "properties":{"entries":{"type":"array","value":[
    {"errorCode":"F.518","errorDescription":"Vorlauffühler","priority":"error","timestamp":"2026-11-28T08:00:00Z"}
  ]}}},
 {"feature":"heating.sensors.temperature.outside","deviceId":"9","timestamp":"2020-01-01T00:00:00Z",
  "properties":{"value":{"type":"number","unit":"celsius","value":99}}}
]}`

func featuresFrom(t *testing.T, s string) []feature {
	t.Helper()
	var answer struct {
		Data []feature `json:"data"`
	}
	if err := json.Unmarshal([]byte(s), &answer); err != nil {
		t.Fatal(err)
	}
	return answer.Data
}

func TestSnapshotFrom(t *testing.T) {
	s := snapshotFrom(featuresFrom(t, featuresJSON), "0")
	if s.Outside == nil || *s.Outside != 4.2 {
		t.Fatalf("outside = %v", s.Outside)
	}
	if s.Boiler == nil || *s.Boiler != 52 || s.Return == nil || *s.Return != 38 {
		t.Fatalf("boiler/return = %v %v", s.Boiler, s.Return)
	}
	if !s.Updated.Equal(mustTime(t, "2026-11-28T10:00:00Z")) {
		t.Fatalf("updated = %v", s.Updated)
	}
	if s.DHW == nil || s.DHW.Temp == nil || *s.DHW.Temp != 48 || !s.DHW.Charging ||
		s.DHW.Mode != "balanced" || s.DHW.Target == nil || *s.DHW.Target != 50 {
		t.Fatalf("dhw = %+v", s.DHW)
	}
	if s.Burner == nil || !s.Burner.Active || s.Burner.Modulation == nil || *s.Burner.Modulation != 42 ||
		s.Burner.Hours == nil || *s.Burner.Hours != 8518 || s.Burner.Starts == nil || *s.Burner.Starts != 43345 {
		t.Fatalf("burner = %+v", s.Burner)
	}
	if s.Compressor == nil || s.Compressor.Active || s.Compressor.Hours == nil {
		t.Fatalf("compressor = %+v", s.Compressor)
	}
	if len(s.Circuits) != 2 {
		t.Fatalf("circuits = %+v", s.Circuits)
	}
	c := s.Circuits[0]
	if c.Name != "Wohnzimmer" || c.Program != "normal" || c.Mode != "dhwAndHeating" ||
		c.Room == nil || *c.Room != 21.5 || c.Target == nil || *c.Target != 45.2 ||
		c.Curve == nil || c.Curve.Slope != 0.6 || c.Curve.Shift != 4 ||
		c.TempMin == nil || *c.TempMin != 20 || c.TempMax == nil || *c.TempMax != 55 {
		t.Fatalf("circuit 0 = %+v", c)
	}
	if s.Circuits[1].Program != "reduced" || s.Circuits[1].Room != nil {
		t.Fatalf("circuit 1 = %+v", s.Circuits[1])
	}
	if len(s.Errors) != 1 || s.Errors[0].Code != "F.518" || s.Errors[0].Description != "Vorlauffühler" ||
		s.Errors[0].Priority != "error" {
		t.Fatalf("errors = %+v", s.Errors)
	}
}

func TestSnapshotFromFiltersAnotherDevice(t *testing.T) {
	// Device 9 has one outside sensor and nothing else.
	s := snapshotFrom(featuresFrom(t, featuresJSON), "9")
	if s.Outside == nil || *s.Outside != 99 {
		t.Fatalf("outside = %v", s.Outside)
	}
	if len(s.Circuits) != 0 || len(s.Errors) != 0 {
		t.Fatalf("snapshot of device 9 = %+v", s)
	}
}

func TestSnapshotWithoutFeatures(t *testing.T) {
	s := snapshotFrom(nil, "0")
	if s == nil {
		t.Fatal("no snapshot")
	}
	if s.Outside != nil || s.DHW != nil || s.Burner != nil || len(s.Circuits) != 0 || len(s.Errors) != 0 {
		t.Fatalf("snapshot = %+v", s)
	}
}

func TestSnapshotReadsTheNewerDHWSensor(t *testing.T) {
	data := `{"data":[
	 {"feature":"heating.dhw.sensors.temperature.dhwCylinder","deviceId":"0",
	  "properties":{"value":{"type":"number","unit":"celsius","value":47.5}}}
	]}`
	s := snapshotFrom(featuresFrom(t, data), "0")
	if s.DHW == nil || s.DHW.Temp == nil || *s.DHW.Temp != 47.5 {
		t.Fatalf("dhw = %+v", s.DHW)
	}
}

func TestSnapshotIgnoresBrokenProperties(t *testing.T) {
	data := `{"data":[
	 {"feature":"heating.sensors.temperature.outside","deviceId":"0","properties":{"value":{"type":"number","value":"cold"}}},
	 {"feature":"heating.circuits.0.heating.curve","deviceId":"0","properties":{"slope":{"value":0.6}}}
	]}`
	s := snapshotFrom(featuresFrom(t, data), "0")
	if s.Outside != nil {
		t.Fatalf("outside = %v", s.Outside)
	}
	if len(s.Circuits) != 1 || s.Circuits[0].Curve != nil {
		t.Fatalf("circuits = %+v", s.Circuits)
	}
}

func mustTime(t *testing.T, s string) time.Time {
	t.Helper()
	parsed, err := time.Parse(time.RFC3339, s)
	if err != nil {
		t.Fatal(err)
	}
	return parsed
}

func TestSnapshotConvertsFahrenheit(t *testing.T) {
	data := `{"data":[
	 {"feature":"heating.sensors.temperature.outside","deviceId":"0",
	  "properties":{"value":{"type":"number","unit":"fahrenheit","value":39.2}}},
	 {"feature":"heating.circuits.0.temperature.levels","deviceId":"0",
	  "properties":{"min":{"type":"number","unit":"fahrenheit","value":68},"max":{"type":"number","unit":"fahrenheit","value":140}}}
	]}`
	s := snapshotFrom(featuresFrom(t, data), "0")
	if s.Outside == nil || math.Abs(*s.Outside-4) > 0.01 {
		t.Fatalf("outside = %v", s.Outside)
	}
	c := s.Circuits[0]
	if c.TempMin == nil || math.Abs(*c.TempMin-20) > 0.01 || c.TempMax == nil || math.Abs(*c.TempMax-60) > 0.01 {
		t.Fatalf("levels = %v %v", c.TempMin, c.TempMax)
	}
}
