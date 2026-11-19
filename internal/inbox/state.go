package inbox

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"time"
)

const (
	SlotsFile     = "slots.json"
	PinsFile      = "pins.json"
	DismissedFile = "dismissed.json"

	stateDirName = "gh-dash"
)

// StateDir returns where the inbox keeps its files for a host:
// $HERDR_PLUGIN_STATE_DIR when set, else $XDG_STATE_HOME/gh-dash/<host>.
// getenv and home are injected to keep the function pure.
func StateDir(host string, getenv func(string) string, home string) string {
	if dir := getenv("HERDR_PLUGIN_STATE_DIR"); dir != "" {
		return dir
	}
	base := getenv("XDG_STATE_HOME")
	if base == "" {
		base = filepath.Join(home, ".local", "state")
	}
	if host == "" {
		host = "github.com"
	}
	return filepath.Join(base, stateDirName, host)
}

// DefaultStateDir resolves StateDir from the process environment.
func DefaultStateDir(host string) string {
	home, _ := os.UserHomeDir()
	return StateDir(host, os.Getenv, home)
}

// Pin is a pinned item. It stores a snapshot so the row can still be shown
// once the item is merged or closed and no source returns it.
type Pin struct {
	Key       string    `json:"key"`
	URL       string    `json:"url"`
	Title     string    `json:"title"`
	Repo      string    `json:"repo"`
	Number    int       `json:"number,omitempty"`
	Type      string    `json:"type,omitempty"`
	State     string    `json:"state,omitempty"`
	UpdatedAt time.Time `json:"updated_at"`
}

// PinOf snapshots an item as a pin.
func PinOf(item Item) Pin {
	return Pin{
		Key:       item.Key,
		URL:       item.URL,
		Title:     item.Title,
		Repo:      item.Repo,
		Number:    item.Number,
		Type:      item.Type,
		State:     item.State,
		UpdatedAt: item.UpdatedAt,
	}
}

func (p Pin) item() Item {
	return Item{
		Key:       p.Key,
		URL:       p.URL,
		Title:     p.Title,
		Repo:      p.Repo,
		Number:    p.Number,
		Type:      p.Type,
		State:     p.State,
		UpdatedAt: p.UpdatedAt,
		Reasons:   []string{},
	}
}

// TogglePin adds the item to the end of pins, or removes it when already
// pinned. It returns the new list and whether the item is now pinned.
func TogglePin(pins []Pin, item Item) ([]Pin, bool) {
	out := make([]Pin, 0, len(pins)+1)
	removed := false
	for _, p := range pins {
		if p.Key == item.Key {
			removed = true
			continue
		}
		out = append(out, p)
	}
	if removed {
		return out, false
	}
	return append(out, PinOf(item)), true
}

// RefreshPins updates stored pin snapshots from freshly fetched items.
func RefreshPins(pins []Pin, items []Item) ([]Pin, bool) {
	byKey := make(map[string]Item, len(items))
	for _, item := range items {
		byKey[item.Key] = item
	}
	changed := false
	out := make([]Pin, len(pins))
	for i, p := range pins {
		out[i] = p
		if item, ok := byKey[p.Key]; ok {
			fresh := PinOf(item)
			if !pinEqual(fresh, p) {
				out[i] = fresh
				changed = true
			}
		}
	}
	return out, changed
}

func pinEqual(a, b Pin) bool {
	if !a.UpdatedAt.Equal(b.UpdatedAt) {
		return false
	}
	a.UpdatedAt, b.UpdatedAt = time.Time{}, time.Time{}
	return a == b
}

// Dismissed maps an item key to the updated_at it had when it was dismissed.
type Dismissed map[string]time.Time

// Hides reports whether the item is still dismissed. It comes back as soon as
// its updated_at is newer than the one recorded.
func (d Dismissed) Hides(item Item) bool {
	at, ok := d[item.Key]
	if !ok {
		return false
	}
	return !item.UpdatedAt.After(at)
}

// Store reads and writes the inbox state files of one host.
type Store struct {
	Dir string
}

func (s Store) path(name string) string {
	return filepath.Join(s.Dir, name)
}

// SlotsPath returns the location of slots.json.
func (s Store) SlotsPath() string {
	return s.path(SlotsFile)
}

func (s Store) LoadPins() ([]Pin, error) {
	pins := []Pin{}
	err := readJSON(s.path(PinsFile), &pins)
	return pins, err
}

func (s Store) SavePins(pins []Pin) error {
	if pins == nil {
		pins = []Pin{}
	}
	return writeJSONAtomic(s.path(PinsFile), pins)
}

func (s Store) LoadDismissed() (Dismissed, error) {
	dismissed := Dismissed{}
	err := readJSON(s.path(DismissedFile), &dismissed)
	return dismissed, err
}

func (s Store) SaveDismissed(dismissed Dismissed) error {
	if dismissed == nil {
		dismissed = Dismissed{}
	}
	return writeJSONAtomic(s.path(DismissedFile), dismissed)
}

// Dismiss records the item as dismissed at its current updated_at, merging
// with whatever is on disk so the panel and the slot command can both write.
func (s Store) Dismiss(item Item) error {
	dismissed, err := s.LoadDismissed()
	if err != nil {
		return err
	}
	dismissed[item.Key] = item.UpdatedAt
	return s.SaveDismissed(dismissed)
}

// ModTimes returns the modification times of the pin and dismissed files, used
// to notice changes made by another process.
func (s Store) ModTimes() [2]time.Time {
	var times [2]time.Time
	for i, name := range []string{PinsFile, DismissedFile} {
		if info, err := os.Stat(s.path(name)); err == nil {
			times[i] = info.ModTime()
		}
	}
	return times
}

func readJSON(path string, v any) error {
	raw, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		return err
	}
	if len(raw) == 0 {
		return nil
	}
	return json.Unmarshal(raw, v)
}

// writeJSONAtomic writes to a temp file in the same directory and renames it
// over the target, so readers never see a partial file.
func writeJSONAtomic(path string, v any) error {
	raw, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return err
	}
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(dir, ".tmp-*")
	if err != nil {
		return err
	}
	tmpPath := tmp.Name()
	if _, err := tmp.Write(append(raw, '\n')); err != nil {
		tmp.Close()
		os.Remove(tmpPath)
		return err
	}
	if err := tmp.Close(); err != nil {
		os.Remove(tmpPath)
		return err
	}
	if err := os.Rename(tmpPath, path); err != nil {
		os.Remove(tmpPath)
		return err
	}
	return nil
}
