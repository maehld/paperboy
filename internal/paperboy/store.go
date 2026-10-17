package displayd

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"image"
	"image/png"
	"io/fs"
	"os"
	"path/filepath"
	"strconv"
	"time"
)

// pageImage is a stored, normalized upload.
type pageImage struct {
	img      *image.Paletted
	data     []byte
	hash     string
	uploaded time.Time
}

// deviceState is persisted per MAC address in state.json.
type deviceState struct {
	APIKey         string    `json:"api_key"`
	FriendlyID     string    `json:"friendly_id"`
	Page           int       `json:"page"`
	LastInput      time.Time `json:"last_input,omitempty"`
	LastSeen       time.Time `json:"last_seen,omitempty"`
	BatteryVoltage string    `json:"battery_voltage,omitempty"`
	RSSI           string    `json:"rssi,omitempty"`
	Firmware       string    `json:"firmware,omitempty"`
	Model          string    `json:"model,omitempty"`
	WakeReason     string    `json:"wake_reason,omitempty"`
	LastError      string    `json:"last_error,omitempty"` // OpenDisplay: last push error, empty after success
}

func imagePath(dir, profile string, page int) string {
	return filepath.Join(dir, "images", profile, strconv.Itoa(page)+".png")
}

func newPageImage(img *image.Paletted, uploaded time.Time) *pageImage {
	data := encodePNG(img)
	sum := sha256.Sum256(data)
	return &pageImage{img: img, data: data, hash: hex.EncodeToString(sum[:6]), uploaded: uploaded}
}

// loadImages reads stored pages; the file modification time is the upload time.
func loadImages(dir string, profiles map[string]Profile) (map[string][]*pageImage, error) {
	images := map[string][]*pageImage{}
	for name, p := range profiles {
		for page := 0; page < maxPages; page++ {
			path := imagePath(dir, name, page)
			info, err := os.Stat(path)
			if errors.Is(err, fs.ErrNotExist) {
				break
			}
			if err != nil {
				return nil, err
			}
			f, err := os.Open(path)
			if err != nil {
				return nil, err
			}
			img, err := png.Decode(f)
			f.Close()
			if err != nil || img.Bounds().Dx() != p.Width || img.Bounds().Dy() != p.Height {
				break // outdated or damaged file: wait for the next upload
			}
			images[name] = append(images[name], newPageImage(normalize(img, p), info.ModTime()))
		}
	}
	return images, nil
}

func writeAtomic(path string, data []byte) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

func loadState(path string) (map[string]*deviceState, error) {
	state := map[string]*deviceState{}
	data, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return state, nil
	}
	if err != nil {
		return nil, err
	}
	if err := json.Unmarshal(data, &state); err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	return state, nil
}
