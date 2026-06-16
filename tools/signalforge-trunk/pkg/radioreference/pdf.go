package radioreference

import (
	"fmt"
	"os"
	"regexp"
	"strconv"
	"strings"

	"github.com/ledongthuc/pdf"
	"github.com/projectseven-co-ltd/p7-scanner/tools/signalforge-trunk/pkg/config"
)

var (
	reFreq     = regexp.MustCompile(`\b(\d{3}\.\d{2,5})c?\b`)
	reSysID    = regexp.MustCompile(`(?i)system\s*id[:\s]+([0-9A-Fa-f]+)`)
	reWACN     = regexp.MustCompile(`(?i)wacn[:\s]+([0-9A-Fa-f]+)`)
)

// ImportPDF parses a RadioReference trunk system PDF export.
func ImportPDF(path string) (config.System, error) {
	f, r, err := pdf.Open(path)
	if err != nil {
		return config.System{}, err
	}
	defer f.Close()

	var text strings.Builder
	total := r.NumPage()
	for page := 1; page <= total; page++ {
		p := r.Page(page)
		if p.V.IsNull() {
			continue
		}
		content, err := p.GetPlainText(nil)
		if err != nil {
			continue
		}
		text.WriteString(content)
		text.WriteString("\n")
	}
	return parsePDFText(text.String()), nil
}

func parsePDFText(raw string) config.System {
	sys := OKWINPreset()
	lines := strings.Split(raw, "\n")
	inSites := false
	for _, line := range lines {
		trim := strings.TrimSpace(line)
		lower := strings.ToLower(trim)
		if strings.Contains(lower, "system name") {
			if name := extractAfterColon(trim); name != "" {
				sys.Name = name
			}
		}
		if m := reSysID.FindStringSubmatch(trim); len(m) == 2 {
			sys.SysID = strings.ToUpper(m[1])
		}
		if m := reWACN.FindStringSubmatch(trim); len(m) == 2 {
			sys.WACN = strings.ToUpper(m[1])
			sys.NAC = sys.WACN
		}
		if strings.Contains(lower, "sites and frequencies") {
			inSites = true
			continue
		}
		if inSites && strings.Contains(lower, "talkgroups") {
			inSites = false
		}
		if !inSites {
			continue
		}
		site := parseSiteLine(trim)
		if site.Name != "" {
			sys.Sites = append(sys.Sites, site)
		}
	}
	sys.ControlChannels = collectControlChannels(sys)
	return sys
}

func parseSiteLine(line string) config.Site {
	freqs := reFreq.FindAllString(line, -1)
	if len(freqs) == 0 {
		return config.Site{}
	}
	parts := strings.Fields(line)
	name := "site"
	county := ""
	if len(parts) >= 4 {
		name = parts[2]
		county = parts[3]
	}
	rfss, siteID := 1, 0
	if len(parts) > 0 {
		if v, err := strconv.Atoi(strings.Trim(parts[0], "()")); err == nil {
			rfss = v
		}
	}
	if len(parts) > 1 {
		if v, err := strconv.Atoi(strings.Trim(parts[1], "()")); err == nil {
			siteID = v
		}
	}
	var frequencies []string
	for _, f := range freqs {
		frequencies = append(frequencies, strings.TrimSuffix(f, "c"))
		if strings.HasSuffix(strings.ToLower(line), f+"c") || strings.Contains(line, f+"c") {
			frequencies[len(frequencies)-1] = f + "c"
		}
	}
	return config.Site{
		RFSS:        rfss,
		SiteID:      siteID,
		Name:        name,
		County:      county,
		Include:     true,
		Frequencies: frequencies,
	}
}

func extractAfterColon(line string) string {
	idx := strings.Index(line, ":")
	if idx < 0 {
		return ""
	}
	return strings.TrimSpace(line[idx+1:])
}

// ImportFromPaths imports PDF and/or CSV paths into config.
func ImportFromPaths(cfg *config.Config, pdfPath, csvPath, name, sysid, csvDir string, force bool) error {
	sys := OKWINPreset()
	if pdfPath != "" {
		parsed, err := ImportPDF(pdfPath)
		if err != nil {
			return fmt.Errorf("pdf import: %w", err)
		}
		sys = parsed
	}
	var tgRows [][]string
	if csvPath != "" {
		if isBundleCSV(csvPath) {
			parsed, rows, err := ImportCSVBundle(csvPath, name, sysid)
			if err != nil {
				return fmt.Errorf("csv bundle: %w", err)
			}
			if pdfPath == "" {
				sys = parsed
			}
			tgRows = rows
		} else {
			rows, err := ImportNativeTalkgroupCSV(csvPath, name, sysid)
			if err != nil {
				return err
			}
			tgRows = rows
		}
	}
	if name != "" {
		sys.Name = name
	}
	if sysid != "" {
		sys.SysID = sysid
	}
	if !force {
		for _, existing := range cfg.Trunking.Systems {
			if strings.EqualFold(existing.Name, sys.Name) {
				return fmt.Errorf("system %q already exists; use --force to overwrite", sys.Name)
			}
		}
	}
	return MergeSystem(cfg, sys, tgRows, csvDir)
}

func isBundleCSV(path string) bool {
	data, err := os.ReadFile(path)
	if err != nil {
		return false
	}
	return strings.Contains(strings.ToLower(string(data)), "# section:")
}
