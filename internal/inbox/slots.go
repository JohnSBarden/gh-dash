package inbox

import (
	"errors"
	"fmt"
	"os"
	"reflect"
	"time"
)

// Slot is one numbered row, as exported to slots.json.
type Slot struct {
	Slot int `json:"slot"`
	Item
}

// SlotsDoc is the content of slots.json.
type SlotsDoc struct {
	GeneratedAt time.Time `json:"generated_at"`
	Host        string    `json:"host"`
	Slots       []Slot    `json:"slots"`
}

// SlotsOf numbers the first MaxSlots rows of the arranged list.
func SlotsOf(items []Item) []Slot {
	n := min(len(items), MaxSlots)
	slots := make([]Slot, n)
	for i := 0; i < n; i++ {
		slots[i] = Slot{Slot: i + 1, Item: items[i]}
	}
	return slots
}

// SlotsEqual reports whether two slot lists describe the same rows.
func SlotsEqual(a, b []Slot) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i].Slot != b[i].Slot || !a[i].UpdatedAt.Equal(b[i].UpdatedAt) {
			return false
		}
		x, y := a[i].Item, b[i].Item
		x.UpdatedAt, y.UpdatedAt = time.Time{}, time.Time{}
		if !reflect.DeepEqual(x, y) {
			return false
		}
	}
	return true
}

// WriteSlots atomically replaces slots.json.
func (s Store) WriteSlots(host string, slots []Slot, now time.Time) error {
	if slots == nil {
		slots = []Slot{}
	}
	return writeJSONAtomic(s.SlotsPath(), SlotsDoc{
		GeneratedAt: now.UTC(),
		Host:        host,
		Slots:       slots,
	})
}

// ErrNoSlots means slots.json has not been written yet.
var ErrNoSlots = errors.New("no slots file")

// ReadSlots loads slots.json.
func (s Store) ReadSlots() (SlotsDoc, error) {
	var doc SlotsDoc
	if _, err := os.Stat(s.SlotsPath()); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return doc, fmt.Errorf("%w at %s (is gh dash running?)", ErrNoSlots, s.SlotsPath())
		}
		return doc, err
	}
	err := readJSON(s.SlotsPath(), &doc)
	return doc, err
}

// Find returns the item in slot n.
func (d SlotsDoc) Find(n int) (Item, error) {
	if n < 1 || n > MaxSlots {
		return Item{}, fmt.Errorf("slot must be between 1 and %d", MaxSlots)
	}
	for _, slot := range d.Slots {
		if slot.Slot == n {
			return slot.Item, nil
		}
	}
	return Item{}, fmt.Errorf("slot %d is empty (%d slots filled)", n, len(d.Slots))
}
