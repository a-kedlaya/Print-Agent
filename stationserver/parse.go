// Package stationserver parses station order documents (HTML or plain text
// extracted from .docx files) into the structured types the mock Palate
// server queues as print jobs.
//
// # Supported HTML format
//
// A <table> whose rows have these columns (header names are matched
// case-insensitively, any order):
//
//	qty | item / dish / name | notes | station | price / unit_price
//
// Alternatively, <li> elements whose text matches the plain-text format below
// and optionally carry a data-station attribute.
//
// # Supported plain-text format (also used for docx extracted text)
//
// One item per line:
//
//	<qty>x <name> [(<notes>)] : <station> : <price>
//
// Examples:
//
//	2x Paneer Tikka : Tandoor : 240
//	4x Butter Naan : Tandoor : 60
//	1x Veg Hakka Noodles (no onion) : Wok : 180
//	2x Masala Chai : Pass : Rs 40
//
// Order metadata lines (optional, anywhere in the document):
//
//	Order: 1042
//	Table: T4
//	Tax: 5%
//	Service: Rs 50
//	Bill-Printer: p-bill
//	Restaurant: Palate Kitchen
//	Footer: GSTIN: 29ABCDE1234F1Z5
package stationserver

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"
	"unicode"
)

// Item is one parsed line item.
type Item struct {
	Quantity  int
	Name      string
	Notes     string
	Station   string
	PrinterID string // looked up from station name via StationPrinters map
	UnitPrice int    // paise (0 if not given)
}

// Order is a fully parsed order document.
type Order struct {
	OrderNumber   string
	Table         string
	Restaurant    string
	Footer        string
	BillPrinter   string
	TaxRate       int // basis points (500 = 5.00%)
	ServiceCharge int // paise
	PlacedAt      time.Time
	Items         []Item
}

// ParseHTML parses an HTML document body into an Order.
// stationPrinters maps lowercase station names to printer IDs.
func ParseHTML(html string, stationPrinters map[string]string) (*Order, error) {
	o := &Order{PlacedAt: time.Now().UTC()}
	
	// Extract metadata from the original HTML (before any stripping)
	parseMetaLines(html, o)

	// Try table first.
	if items := parseHTMLTable(html, stationPrinters); len(items) > 0 {
		o.Items = items
		return o, nil
	}
	// Fall back to <li> parsing.
	if items := parseHTMLList(html, stationPrinters); len(items) > 0 {
		o.Items = items
		return o, nil
	}
	// Fall back to plain-text parse of the visible text.
	text := htmlToText(html)
	items := parsePlainLines(text, stationPrinters)
	if len(items) == 0 {
		return nil, fmt.Errorf("stationserver: no items found in document")
	}
	o.Items = items
	return o, nil
}

// ParseText parses plain text (e.g. extracted from a .docx) into an Order.
func ParseText(text string, stationPrinters map[string]string) (*Order, error) {
	o := &Order{PlacedAt: time.Now().UTC()}
	parseMetaLines(text, o)
	items := parsePlainLines(text, stationPrinters)
	if len(items) == 0 {
		return nil, fmt.Errorf("stationserver: no items found in document")
	}
	o.Items = items
	return o, nil
}

// --- meta parsing ------------------------------------------------------------

var metaPatterns = []struct {
	re  *regexp.Regexp
	set func(*Order, string)
}{
	{regexp.MustCompile(`(?i)order[:\s#]+(\S+)`), func(o *Order, v string) { o.OrderNumber = stripHTMLTags(v) }},
	{regexp.MustCompile(`(?i)table[:\s]+(\S+)`), func(o *Order, v string) { o.Table = stripHTMLTags(v) }},
	{regexp.MustCompile(`(?i)restaurant[:\s]+(.+)`), func(o *Order, v string) { o.Restaurant = strings.TrimSpace(stripHTMLTags(v)) }},
	{regexp.MustCompile(`(?i)footer[:\s]+(.+)`), func(o *Order, v string) { o.Footer = strings.TrimSpace(stripHTMLTags(v)) }},
	{regexp.MustCompile(`(?i)bill.?printer[:\s]+(\S+)`), func(o *Order, v string) { o.BillPrinter = stripHTMLTags(v) }},
	{regexp.MustCompile(`(?i)tax[:\s]+([\d.]+)\s*%`), func(o *Order, v string) {
		if f, err := strconv.ParseFloat(v, 64); err == nil {
			o.TaxRate = int(f * 100)
		}
	}},
	{regexp.MustCompile(`(?i)service[:\s]+(?:rs\s*)?([\d.]+)`), func(o *Order, v string) {
		if f, err := strconv.ParseFloat(v, 64); err == nil {
			o.ServiceCharge = int(f * 100)
		}
	}},
}

// stripHTMLTags removes HTML tags from a string (simpler version for metadata)
func stripHTMLTags(s string) string {
	s = regexp.MustCompile(`<[^>]+>`).ReplaceAllString(s, "")
	return strings.TrimSpace(s)
}

func parseMetaLines(text string, o *Order) {
	for _, p := range metaPatterns {
		if m := p.re.FindStringSubmatch(text); m != nil {
			p.set(o, m[1])
		}
	}
}

// --- HTML table parser -------------------------------------------------------

// colIndex finds the column index for a header name.
func colIndex(headers []string, candidates ...string) int {
	for i, h := range headers {
		h = strings.ToLower(strings.TrimSpace(h))
		for _, c := range candidates {
			if strings.Contains(h, c) {
				return i
			}
		}
	}
	return -1
}

func parseHTMLTable(html string, sp map[string]string) []Item {
	// Extract <tr> blocks.
	rows := reAll(`(?is)<tr[^>]*>(.*?)</tr>`, html)
	if len(rows) < 2 {
		return nil
	}
	// First row: headers.
	headers := reAll(`(?is)<t[hd][^>]*>(.*?)</t[hd]>`, rows[0])
	for i, h := range headers {
		headers[i] = htmlToText(h)
	}
	qtyCol   := colIndex(headers, "qty", "quantity", "count")
	nameCol  := colIndex(headers, "item", "dish", "name")
	notesCol := colIndex(headers, "note", "modifier", "special")
	stCol    := colIndex(headers, "station", "section", "kitchen")
	priceCol := colIndex(headers, "price", "rate", "amount")
	if nameCol < 0 {
		return nil
	}
	var items []Item
	for _, row := range rows[1:] {
		cells := reAll(`(?is)<td[^>]*>(.*?)</td>`, row)
		if len(cells) == 0 {
			continue
		}
		cell := func(i int) string {
			if i >= 0 && i < len(cells) {
				return strings.TrimSpace(htmlToText(cells[i]))
			}
			return ""
		}
		name := cell(nameCol)
		if name == "" {
			continue
		}
		qty := 1
		if s := cell(qtyCol); s != "" {
			if n, err := strconv.Atoi(s); err == nil && n > 0 {
				qty = n
			}
		}
		station := cell(stCol)
		price   := parsePaise(cell(priceCol))
		it := Item{
			Quantity:  qty,
			Name:      name,
			Notes:     cell(notesCol),
			Station:   station,
			UnitPrice: price,
		}
		it.PrinterID = sp[strings.ToLower(station)]
		items = append(items, it)
	}
	return items
}

// --- HTML list parser --------------------------------------------------------

func parseHTMLList(html string, sp map[string]string) []Item {
	lis := reAll(`(?is)<li([^>]*)>(.*?)</li>`, html)
	var items []Item
	for _, li := range lis {
		// li[0] is the whole match including attributes; we need attrs and text separately.
		// reAll returns the full match — redo with submatch.
		_ = li // handled below
	}
	re := regexp.MustCompile(`(?is)<li([^>]*)>(.*?)</li>`)
	for _, m := range re.FindAllStringSubmatch(html, -1) {
		attrs := m[1]
		text  := strings.TrimSpace(htmlToText(m[2]))
		if text == "" {
			continue
		}
		// data-station attribute overrides parsed station.
		stationAttr := ""
		if sm := regexp.MustCompile(`(?i)data-station="([^"]+)"`).FindStringSubmatch(attrs); sm != nil {
			stationAttr = sm[1]
		}
		it, ok := parseLine(text, sp)
		if !ok {
			continue
		}
		if stationAttr != "" {
			it.Station   = stationAttr
			it.PrinterID = sp[strings.ToLower(stationAttr)]
		}
		items = append(items, it)
	}
	return items
}

// --- plain-text line parser --------------------------------------------------

// itemLine matches:  2x Paneer : Tandoor : 240  OR  2x Paneer : 240
var itemLine = regexp.MustCompile(
	`(?i)^(\d+)\s*[xX×]\s+` + // qty
		`([^:]+)` + // name (everything before first colon)
		`(?:\s*\(([^)]*)\))?` + // optional (notes)
		`\s*:\s*([A-Za-z][A-Za-z ]+?)?` + // optional : Station
		`\s*(?::\s*)?(?:Rs\.?\s*)?([\d.]+)?` + // optional : price (with or without Rs)
		`\s*$`,
)

func parseLine(line string, sp map[string]string) (Item, bool) {
	m := itemLine.FindStringSubmatch(strings.TrimSpace(line))
	if m == nil {
		return Item{}, false
	}
	qty, _ := strconv.Atoi(m[1])
	if qty < 1 {
		qty = 1
	}
	name     := strings.TrimSpace(m[2])
	notes    := strings.TrimSpace(m[3])
	station  := strings.TrimSpace(m[4])
	priceStr := ""
	if len(m) > 5 {
		priceStr = m[5]
	}
	
	it := Item{
		Quantity:  qty,
		Name:      name,
		Notes:     notes,
		Station:   station,
		UnitPrice: parsePaise(priceStr),
		PrinterID: sp[strings.ToLower(station)],
	}
	return it, name != ""
}

// itemStart matches the beginning of an item token: digit(s) followed by x/X/×
// and whitespace. Used to split lines that contain multiple concatenated items.
var itemStart = regexp.MustCompile(`\d+\s*[xX×]\s+`)

func parsePlainLines(text string, sp map[string]string) []Item {
	// Pre-process: if a line contains multiple items concatenated without
	// newlines (e.g. "2x Paneer — Tandoor — Rs 240 4x Naan — Tandoor"),
	// insert newlines at item boundaries so each is parsed separately.
	text = splitConcatenatedItems(text)

	var items []Item
	for _, line := range strings.Split(text, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		if it, ok := parseLine(line, sp); ok {
			items = append(items, it)
		}
	}
	return items
}

// splitConcatenatedItems inserts newlines before item-start patterns
// (e.g. "4x ") that appear mid-line, so each item gets its own line.
//
// It avoids false splits by only splitting when a digit+x pattern appears
// after what looks like the end of a previous item (a digit, a letter, or a
// closing paren — i.e. not at the very start of the line).
func splitConcatenatedItems(text string) string {
	var out strings.Builder
	for _, line := range strings.Split(text, "\n") {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			out.WriteString(line)
			out.WriteByte('\n')
			continue
		}
		// Find all item-start positions in this line.
		locs := itemStart.FindAllStringIndex(trimmed, -1)
		if len(locs) <= 1 {
			// Zero or one item start: nothing to split.
			out.WriteString(line)
			out.WriteByte('\n')
			continue
		}
		// Multiple item starts found. The first is the beginning of the
		// first item; subsequent ones that are preceded by a space (and
		// the char before that space is a digit, letter, or ')') are
		// split points.
		prev := 0
		for i, loc := range locs {
			if i == 0 {
				continue
			}
			pos := loc[0]
			// Only split if there's whitespace before this match and the
			// character before that whitespace ends a price, station, or
			// notes section.
			if pos > 0 && trimmed[pos-1] == ' ' {
				// Write the segment up to the split.
				out.WriteString(trimmed[prev:pos])
				out.WriteByte('\n')
				prev = pos
			}
		}
		out.WriteString(trimmed[prev:])
		out.WriteByte('\n')
	}
	return out.String()
}

// --- helpers -----------------------------------------------------------------

func reAll(pattern, s string) []string {
	re := regexp.MustCompile(pattern)
	var out []string
	for _, m := range re.FindAllStringSubmatch(s, -1) {
		out = append(out, m[1])
	}
	return out
}

// htmlToText strips HTML tags and decodes basic entities.
// Block-level elements (p, div, tr, br) are converted to newlines so that
// the plain-text parser can process each line separately.
var tagRE   = regexp.MustCompile(`<[^>]+>`)
var spaceRE = regexp.MustCompile(`[ \t]+`)
var newlineRE = regexp.MustCompile(`\n\n+`)

func htmlToText(s string) string {
	// Convert block-level tags to newlines before stripping all tags.
	blockTags := regexp.MustCompile(`(?i)</(p|div|tr|li|h[1-6]|table)>|<br\s*/?>`)
	s = blockTags.ReplaceAllString(s, "\n")
	
	// Strip remaining tags.
	s = tagRE.ReplaceAllString(s, " ")
	
	// Decode HTML entities.
	s = strings.NewReplacer(
		"&amp;", "&", "&lt;", "<", "&gt;", ">",
		"&quot;", `"`, "&#39;", "'", "&nbsp;", " ",
		"&#160;", " ",
	).Replace(s)
	
	// Collapse multiple spaces on the same line.
	s = spaceRE.ReplaceAllString(s, " ")
	
	// Collapse multiple newlines to single newlines.
	s = newlineRE.ReplaceAllString(s, "\n")
	
	return strings.TrimFunc(s, unicode.IsSpace)
}

// parsePaise converts a price string like "240" or "240.00" to paise.
func parsePaise(s string) int {
	s = strings.TrimSpace(s)
	if s == "" {
		return 0
	}
	f, err := strconv.ParseFloat(s, 64)
	if err != nil {
		return 0
	}
	return int(f * 100)
}
