package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// errNoJob is the job endpoint's 204: the printer knows no job.
var errNoJob = errors.New("no job")

// snapshot is what the widgets draw. It is copied under the plugin's lock and
// never changed afterwards.
type snapshot struct {
	loaded bool
	status statusResponse
	job    jobResponse
	info   infoResponse
	err    error // the last failed request, for the log and the first load
}

type statusResponse struct {
	Printer printerState `json:"printer"`
	Job     *jobStatus   `json:"job"`
}

type printerState struct {
	State        string   `json:"state"`
	TempNozzle   *float64 `json:"temp_nozzle"`
	TargetNozzle *float64 `json:"target_nozzle"`
	TempBed      *float64 `json:"temp_bed"`
	TargetBed    *float64 `json:"target_bed"`
	Status       struct {
		OK      *bool  `json:"ok"`
		Message string `json:"message"`
	} `json:"status_printer"`
}

type jobStatus struct {
	Progress      *float64 `json:"progress"`
	TimeRemaining *int64   `json:"time_remaining"`
	TimePrinting  *int64   `json:"time_printing"`
}

type jobResponse struct {
	Progress      *float64 `json:"progress"`
	TimeRemaining *int64   `json:"time_remaining"`
	TimePrinting  *int64   `json:"time_printing"`
	File          *struct {
		Name        string `json:"name"`
		DisplayName string `json:"display_name"`
	} `json:"file"`
}

type infoResponse struct {
	Name string `json:"name"`
}

// state is the printer state in capitals, as the API reports it.
func (s snapshot) state() string {
	return strings.ToUpper(strings.TrimSpace(s.status.Printer.State))
}

// running reports whether a print is active.
func (s snapshot) running() bool {
	switch s.state() {
	case "PRINTING", "PAUSED":
		return true
	}
	return false
}

func (s snapshot) progress() (float64, bool) {
	if s.status.Job != nil && s.status.Job.Progress != nil {
		return *s.status.Job.Progress, true
	}
	if s.job.Progress != nil {
		return *s.job.Progress, true
	}
	return 0, false
}

func (s snapshot) remaining() (time.Duration, bool) {
	if s.status.Job != nil && s.status.Job.TimeRemaining != nil {
		return time.Duration(*s.status.Job.TimeRemaining) * time.Second, true
	}
	if s.job.TimeRemaining != nil {
		return time.Duration(*s.job.TimeRemaining) * time.Second, true
	}
	return 0, false
}

func (s snapshot) printingTime() (time.Duration, bool) {
	if s.status.Job != nil && s.status.Job.TimePrinting != nil {
		return time.Duration(*s.status.Job.TimePrinting) * time.Second, true
	}
	if s.job.TimePrinting != nil {
		return time.Duration(*s.job.TimePrinting) * time.Second, true
	}
	return 0, false
}

// jobName is the file name of the current or last job.
func (s snapshot) jobName() string {
	if s.job.File == nil {
		return ""
	}
	if s.job.File.DisplayName != "" {
		return s.job.File.DisplayName
	}
	return s.job.File.Name
}

// message is the printer's own error text, when it reports one.
func (s snapshot) message() string {
	if s.status.Printer.Status.OK != nil && *s.status.Printer.Status.OK {
		return ""
	}
	return strings.TrimSpace(s.status.Printer.Status.Message)
}

// signature identifies what the widgets would show. The progress is whole
// percent and the remaining time whole minutes, so a small move does not
// redraw the page.
func (s snapshot) signature() string {
	if !s.loaded {
		return "none"
	}
	var b strings.Builder
	fmt.Fprintf(&b, "%s|", s.state())
	if p, ok := s.progress(); ok {
		fmt.Fprintf(&b, "%.0f|", p)
	}
	if d, ok := s.remaining(); ok {
		fmt.Fprintf(&b, "%d|", int(d.Round(time.Minute)/time.Minute))
	}
	for _, v := range []*float64{
		s.status.Printer.TempNozzle, s.status.Printer.TargetNozzle,
		s.status.Printer.TempBed, s.status.Printer.TargetBed,
	} {
		if v != nil {
			fmt.Fprintf(&b, "%.0f/", *v)
		} else {
			b.WriteString("-/")
		}
	}
	fmt.Fprintf(&b, "%s|%s", s.jobName(), s.message())
	return b.String()
}

// get reads one endpoint. The first request carries the API key as
// X-Api-Key, which PrusaLink accepts as well. A 401 switches to Digest for
// this and every later request; a stale nonce refreshes the challenge once.
func (p *prusalink) get(ctx context.Context, path string, out any) error {
	client := p.client
	if client == nil {
		client = http.DefaultClient
	}
	for attempt := 0; attempt < 3; attempt++ {
		ctx, cancel := context.WithTimeout(ctx, requestTimeout)
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, p.base+path, nil)
		if err != nil {
			cancel()
			return err
		}
		req.Header.Set("User-Agent", userAgent)
		req.Header.Set("Accept", "application/json")
		if ch := p.auth.pending(); ch != nil {
			header, err := p.auth.header(ch, http.MethodGet, path, digestUser, p.apiKey)
			if err != nil {
				cancel()
				return err
			}
			req.Header.Set("Authorization", header)
		} else {
			req.Header.Set("X-Api-Key", p.apiKey)
		}

		resp, err := client.Do(req)
		if err != nil {
			cancel()
			return err
		}
		if resp.StatusCode == http.StatusUnauthorized {
			ch, parseErr := parseChallenge(resp.Header.Get("WWW-Authenticate"))
			io.Copy(io.Discard, io.LimitReader(resp.Body, 4<<10))
			resp.Body.Close()
			cancel()
			if parseErr != nil {
				return fmt.Errorf("%s: %s: %w", path, resp.Status, parseErr)
			}
			p.auth.remember(ch)
			continue
		}
		if resp.StatusCode == http.StatusNoContent {
			resp.Body.Close()
			cancel()
			return errNoJob
		}
		if resp.StatusCode != http.StatusOK {
			err := apiError(resp)
			resp.Body.Close()
			cancel()
			return fmt.Errorf("%s: %w", path, err)
		}
		err = json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(out)
		resp.Body.Close()
		cancel()
		if err != nil {
			return fmt.Errorf("%s: answer: %w", path, err)
		}
		return nil
	}
	return fmt.Errorf("%s: 401 Unauthorized; check the api_key", path)
}

// apiError turns an error answer into one line. PrusaLink names a title and a
// text; a bare status is enough otherwise.
func apiError(resp *http.Response) error {
	var body struct {
		Title string `json:"title"`
		Text  string `json:"text"`
		Code  string `json:"code"`
	}
	_ = json.NewDecoder(io.LimitReader(resp.Body, 64<<10)).Decode(&body)
	msg := strings.TrimSpace(strings.Join(compact([]string{body.Title, body.Text}), ": "))
	if msg == "" {
		msg = resp.Status
	}
	if body.Code != "" {
		msg += " (" + body.Code + ")"
	}
	return errors.New(msg)
}

func compact(list []string) []string {
	out := list[:0]
	for _, s := range list {
		if s = strings.TrimSpace(s); s != "" {
			out = append(out, s)
		}
	}
	return out
}
