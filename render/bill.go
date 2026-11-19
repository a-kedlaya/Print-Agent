package render

import (
	"errors"
	"fmt"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/ShreyasBairyKS/Palate-Print-Agent/job"
)

// Bill turns one customer bill into ESC/POS bytes. It is the paper track, like
// Render: no socket, no config, no clock. It reuses the same layout primitives
// as a kitchen ticket, so a bill with Kannada, Devanagari or Tamil dish names
// prints through the same raster path and carries the same banners when it is
// reprinted or moved to a backup.
func Bill(b job.Bill, width job.Width) ([]byte, error) {
	if !width.Valid() {
		return nil, errors.New("render: width must be 58 or 80 mm")
	}
	if err := checkBill(b); err != nil {
		return nil, err
	}
	title := b.Title
	if title == "" {
		title = "CUSTOMER BILL"
	}
	cols := columns(width)
	r := &renderer{cols: cols, paperDots: cols * colDots}
	bb := &r.b
	bb.Write(cmdInit)
	bb.Write(cmdCodePage)

	r.banners(b.Duplicate, b.DuplicateReason, b.MovedFrom, b.FallbackFrom, b.Delayed)

	// Header: the restaurant, then the document title, both centred and bold.
	if b.Restaurant != "" {
		bb.Write(cmdAlignCentre)
		bb.Write(cmdBoldOn)
		r.block(b.Restaurant, cols, 0, "", "", style{bold: true, centre: true})
		bb.Write(cmdBoldOff)
	}
	bb.Write(cmdAlignCentre)
	bb.Write(cmdBoldOn)
	r.block(strings.ToUpper(title), cols, 0, "", "", style{bold: true, centre: true})
	bb.Write(cmdBoldOff)
	bb.Write(cmdAlignLeft)

	// Bill metadata: bill number and time on one line, table and order on
	// the next, then optional server and guest count.
	r.spread("Bill #"+b.BillNumber, b.PlacedAt.Format("02 Jan 2006 15:04"))
	table := "Takeaway"
	if b.Table != "" {
		table = "Table " + b.Table
	}
	r.spread(table, "Order #"+b.OrderNumber)
	if b.Server != "" {
		r.text("Server: " + b.Server)
	}
	if b.Guests > 0 {
		r.text(fmt.Sprintf("Guests: %d", b.Guests))
	}

	r.rule()

	// Items: "<qty> x <name>" flush left, the line amount flush right.
	for _, it := range b.Items {
		r.spread(fmt.Sprintf("%d x %s", it.Quantity, it.Name), money(b.Currency, it.Amount))
		if it.Notes != "" {
			r.block(it.Notes, cols, 0, "  ", "  ", style{})
		}
	}
	r.rule()

	// Totals, the total line emphasised.
	r.spread("Subtotal", money(b.Currency, b.Subtotal))
	if b.TaxRate != 0 || b.Tax != 0 {
		r.spread("Tax ("+percent(b.TaxRate)+")", money(b.Currency, b.Tax))
	}
	if b.ServiceCharge != 0 {
		r.spread("Service Charge", money(b.Currency, b.ServiceCharge))
	}
	if b.Discount != 0 {
		r.spread("Discount", "-"+money(b.Currency, b.Discount))
	}
	bb.Write(cmdBoldOn)
	r.spread("TOTAL", money(b.Currency, b.Total))
	bb.Write(cmdBoldOff)

	// Payments, if any, then change.
	if len(b.Payments) > 0 {
		r.rule()
		for _, p := range b.Payments {
			r.spread(p.Method, money(b.Currency, p.Amount))
		}
		if b.Change != 0 {
			r.spread("Change", money(b.Currency, b.Change))
		}
	}

	r.rule()
	bb.Write(cmdAlignCentre)
	r.block("Thank you!", cols, 0, "", "", style{centre: true})
	if b.Footer != "" {
		r.block(b.Footer, cols, 0, "", "", style{centre: true})
	}
	bb.Write(cmdAlignLeft)

	bb.Write(cmdFeedLines)
	bb.Write(cmdCut)
	if r.err != nil {
		return nil, r.err
	}
	return bb.Bytes(), nil
}

// checkBill rejects a bill that can't be printed, naming the field, and
// checks that any text needing the raster path can be shaped.
func checkBill(b job.Bill) error {
	if b.OrderNumber == "" && b.BillNumber == "" {
		return errors.New("render: bill needs a bill number or order number")
	}
	if len(b.Items) == 0 {
		return errors.New("render: bill needs at least one item")
	}
	fields := []struct{ name, text string }{
		{"restaurant", b.Restaurant}, {"title", b.Title}, {"bill_number", b.BillNumber},
		{"table", b.Table}, {"order_number", b.OrderNumber}, {"server", b.Server}, {"footer", b.Footer},
	}
	for i, it := range b.Items {
		fields = append(fields,
			struct{ name, text string }{fmt.Sprintf("items[%d].name", i), it.Name},
			struct{ name, text string }{fmt.Sprintf("items[%d].notes", i), it.Notes})
		if it.Quantity < 1 {
			return fmt.Errorf("render: items[%d] has quantity %d", i, it.Quantity)
		}
		if it.UnitPrice < 0 || it.Amount < 0 {
			return fmt.Errorf("render: items[%d] has a negative price", i)
		}
	}
	for _, f := range fields {
		if !utf8.ValidString(f.text) {
			return fmt.Errorf("render: %s is not valid UTF-8", f.name)
		}
		if needsRaster(f.text) {
			if _, err := itemise([]rune(sanitise(f.text))); err != nil {
				return fmt.Errorf("render: %s: %w", f.name, err)
			}
		}
	}
	if b.Subtotal < 0 || b.Tax < 0 || b.ServiceCharge < 0 || b.Discount < 0 || b.Total < 0 {
		return errors.New("render: bill totals must not be negative")
	}
	return nil
}

// money formats paise as a decimal amount with the currency prefix, e.g.
// "Rs 240.00". A negative is rare (a discount is shown with a leading "-"),
// but handled so the sign lands before the currency.
func money(currency string, paise int) string {
	neg := paise < 0
	if neg {
		paise = -paise
	}
	s := fmt.Sprintf("%d.%02d", paise/100, paise%100)
	if currency != "" {
		s = currency + s
	}
	if neg {
		s = "-" + s
	}
	return s
}

// percent renders a basis-points rate as a percentage label: 500 -> "5%",
// 1250 -> "12.50%".
func percent(basis int) string {
	if basis%100 == 0 {
		return strconv.Itoa(basis/100) + "%"
	}
	return fmt.Sprintf("%.2f%%", float64(basis)/100)
}
