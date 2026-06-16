package trunking

import (
	"encoding/csv"
	"fmt"
	"os"
	"strconv"
	"strings"
	"sync"
)

// Talkgroup holds decoded talkgroup metadata from RR CSV.
type Talkgroup struct {
	Decimal     int
	Hex         string
	Mode        string
	AlphaTag    string
	Description string
	Tag         string
	Group       string
}

// TalkgroupDB maps talkgroup IDs to labels.
type TalkgroupDB struct {
	mu    sync.RWMutex
	byDEC map[int]Talkgroup
}

func NewTalkgroupDB() *TalkgroupDB {
	return &TalkgroupDB{byDEC: make(map[int]Talkgroup)}
}

func (db *TalkgroupDB) LoadCSV(path string) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()
	rows, err := csv.NewReader(f).ReadAll()
	if err != nil {
		return err
	}
	if len(rows) < 2 {
		return fmt.Errorf("talkgroup csv has no data rows")
	}
	header := normalizeHeader(rows[0])
	decIdx := colIndex(header, "decimal", "dec")
	if decIdx < 0 {
		return fmt.Errorf("talkgroup csv missing decimal column")
	}
	db.mu.Lock()
	defer db.mu.Unlock()
	for _, row := range rows[1:] {
		if decIdx >= len(row) {
			continue
		}
		dec, err := strconv.Atoi(strings.TrimSpace(row[decIdx]))
		if err != nil {
			continue
		}
		tg := Talkgroup{Decimal: dec}
		tg.Hex = field(row, header, "hex")
		tg.Mode = field(row, header, "mode")
		tg.AlphaTag = field(row, header, "alpha tag", "alpha_tag")
		tg.Description = field(row, header, "description", "desc")
		tg.Tag = field(row, header, "tag", "category")
		tg.Group = field(row, header, "group")
		db.byDEC[dec] = tg
	}
	return nil
}

func (db *TalkgroupDB) Lookup(dec int) (Talkgroup, bool) {
	db.mu.RLock()
	defer db.mu.RUnlock()
	tg, ok := db.byDEC[dec]
	return tg, ok
}

func (db *TalkgroupDB) Count() int {
	db.mu.RLock()
	defer db.mu.RUnlock()
	return len(db.byDEC)
}

func normalizeHeader(row []string) []string {
	out := make([]string, len(row))
	for i, col := range row {
		out[i] = strings.ToLower(strings.TrimSpace(col))
	}
	return out
}

func colIndex(header []string, names ...string) int {
	for i, col := range header {
		for _, name := range names {
			if col == name {
				return i
			}
		}
	}
	return -1
}

func field(row, header []string, names ...string) string {
	idx := colIndex(header, names...)
	if idx < 0 || idx >= len(row) {
		return ""
	}
	return strings.TrimSpace(row[idx])
}
