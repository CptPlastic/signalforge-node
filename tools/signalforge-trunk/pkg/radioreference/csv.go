package radioreference

import (
	"encoding/csv"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/projectseven-co-ltd/p7-scanner/tools/signalforge-trunk/pkg/config"
)

// OKWINPreset returns default metadata for RadioReference sid 6949.
func OKWINPreset() config.System {
	return config.DefaultOKWIN()
}

// ImportCSVBundle parses a multi-section RR-style CSV bundle into a system config.
func ImportCSVBundle(path string, overrideName, overrideSysID string) (config.System, [][]string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return config.System{}, nil, err
	}
	return parseCSVBundle(string(data), overrideName, overrideSysID)
}

// ImportNativeTalkgroupCSV parses RR's flat talkgroup export.
func ImportNativeTalkgroupCSV(path, name, sysid string) ([][]string, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	rows, err := csv.NewReader(f).ReadAll()
	if err != nil {
		return nil, err
	}
	if len(rows) == 0 {
		return nil, fmt.Errorf("empty talkgroup csv")
	}
	header := normalizeHeader(rows[0])
	if !hasColumn(header, "decimal") && !hasColumn(header, "dec") {
		return nil, fmt.Errorf("not a native RR talkgroup csv")
	}
	_ = name
	_ = sysid
	return rows, nil
}

// MergeSystem writes system + talkgroup CSV alongside trunk config.
func MergeSystem(cfg *config.Config, sys config.System, talkgroupRows [][]string, csvDir string) error {
	if csvDir == "" {
		csvDir = "."
	}
	if err := os.MkdirAll(csvDir, 0o755); err != nil {
		return err
	}
	csvName := sys.TalkgroupCSV
	if csvName == "" {
		csvName = fmt.Sprintf("talkgroups-%s-%s.csv", slug(sys.Name), sys.SysID)
		sys.TalkgroupCSV = csvName
	}
	csvPath := filepath.Join(csvDir, csvName)
	if len(talkgroupRows) > 0 {
		if err := writeTalkgroupCSV(csvPath, talkgroupRows); err != nil {
			return err
		}
	}
	replaced := false
	for i, existing := range cfg.Trunking.Systems {
		if strings.EqualFold(existing.Name, sys.Name) {
			cfg.Trunking.Systems[i] = sys
			replaced = true
			break
		}
	}
	if !replaced {
		cfg.Trunking.Systems = append(cfg.Trunking.Systems, sys)
	}
	return nil
}

func parseCSVBundle(text, overrideName, overrideSysID string) (config.System, [][]string, error) {
	sys := OKWINPreset()
	var talkgroupRows [][]string
	section := ""
	for _, line := range strings.Split(text, "\n") {
		trim := strings.TrimSpace(line)
		if trim == "" {
			continue
		}
		if strings.HasPrefix(trim, "#") {
			if strings.Contains(strings.ToLower(trim), "section:") {
				section = strings.TrimSpace(strings.SplitN(trim, ":", 2)[1])
			}
			continue
		}
		switch strings.ToLower(section) {
		case "metadata":
			if strings.EqualFold(strings.Split(trim, ",")[0], "key") {
				continue
			}
			parts := splitCSVLine(trim)
			if len(parts) < 2 {
				continue
			}
			key := strings.ToLower(strings.TrimSpace(parts[0]))
			val := strings.Trim(strings.TrimSpace(parts[1]), `"`)
			switch key {
			case "name":
				sys.Name = val
			case "protocol":
				sys.Protocol = val
			case "sysid":
				sys.SysID = val
			case "wacn":
				sys.WACN = val
				sys.NAC = val
			}
		case "sites":
			if strings.HasPrefix(strings.ToLower(trim), "rfss") {
				continue
			}
			parts := splitCSVLine(trim)
			if len(parts) < 4 {
				continue
			}
			rfss, _ := strconv.Atoi(strings.TrimSpace(parts[0]))
			siteID, _ := strconv.Atoi(strings.TrimSpace(parts[1]))
			site := config.Site{
				RFSS:        rfss,
				SiteID:      siteID,
				Name:        strings.TrimSpace(parts[2]),
				County:      strings.TrimSpace(parts[3]),
				Include:     true,
				Frequencies: parts[4:],
			}
			sys.Sites = append(sys.Sites, site)
		case "talkgroups":
			if talkgroupRows == nil {
				talkgroupRows = [][]string{normalizeTalkgroupHeader(splitCSVLine(trim))}
				continue
			}
			talkgroupRows = append(talkgroupRows, splitCSVLine(trim))
		}
	}
	if overrideName != "" {
		sys.Name = overrideName
	}
	if overrideSysID != "" {
		sys.SysID = overrideSysID
	}
	sys.ControlChannels = collectControlChannels(sys)
	return sys, talkgroupRows, nil
}

func collectControlChannels(sys config.System) []string {
	var out []string
	seen := map[string]struct{}{}
	for _, site := range sys.Sites {
		for _, f := range site.Frequencies {
			lower := strings.ToLower(strings.TrimSpace(f))
			if !strings.HasSuffix(lower, "c") {
				continue
			}
			freq := strings.TrimSuffix(strings.TrimSuffix(lower, "c"), " ")
			if _, ok := seen[freq]; ok {
				continue
			}
			seen[freq] = struct{}{}
			out = append(out, freq)
		}
	}
	return out
}

func writeTalkgroupCSV(path string, rows [][]string) error {
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()
	w := csv.NewWriter(f)
	if err := w.WriteAll(rows); err != nil {
		return err
	}
	w.Flush()
	return w.Error()
}

func normalizeTalkgroupHeader(row []string) []string {
	out := make([]string, len(row))
	copy(out, row)
	for i, col := range out {
		switch strings.ToLower(strings.TrimSpace(col)) {
		case "dec":
			out[i] = "Decimal"
		case "alpha tag":
			out[i] = "Alpha Tag"
		case "desc":
			out[i] = "Description"
		case "category":
			out[i] = "Tag"
		}
	}
	return out
}

func normalizeHeader(row []string) []string {
	out := make([]string, len(row))
	for i, col := range row {
		out[i] = strings.ToLower(strings.TrimSpace(col))
	}
	return out
}

func hasColumn(header []string, name string) bool {
	for _, col := range header {
		if col == name {
			return true
		}
	}
	return false
}

func splitCSVLine(line string) []string {
	r := csv.NewReader(strings.NewReader(line))
	r.LazyQuotes = true
	row, err := r.Read()
	if err != nil {
		return strings.Split(line, ",")
	}
	return row
}

func slug(name string) string {
	name = strings.ToLower(name)
	var b strings.Builder
	for _, r := range name {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {
			b.WriteRune(r)
		} else if r == ' ' || r == '-' {
			b.WriteRune('-')
		}
	}
	return strings.Trim(b.String(), "-")
}
