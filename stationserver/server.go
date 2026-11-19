// Package stationserver wraps the existing mock Palate server and adds a
// station-document upload endpoint and a minimal web UI. The Palate server
// (source) sends station documents — HTML or docx — to the restaurant. This
// server simulates that: you POST a document here, it is parsed into
// per-station kitchen ticket jobs and a combined customer bill, and those
// jobs are queued into the mock Palate server for the agent to pick up and
// print.
package stationserver

import (
	"archive/zip"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"regexp"
	"strings"
	"time"

	"github.com/ShreyasBairyKS/Palate-Print-Agent/api"
	"github.com/ShreyasBairyKS/Palate-Print-Agent/internal/mockserver"
)

// Config wires the station server to the mock Palate server.
type Config struct {
	// MockServer is the running mock Palate server jobs are queued into.
	MockServer *mockserver.Server
	// Restaurant is the restaurant ID used when queuing jobs.
	Restaurant string
	// StationPrinters maps lowercase station names to printer IDs configured
	// on the agent till: {"tandoor":"p-tandoor","wok":"p-wok","pass":"p-pass"}.
	StationPrinters map[string]string
	// BillPrinter is the default printer ID for the combined bill (can be
	// overridden per document with a "Bill-Printer:" metadata line).
	BillPrinter string
	// Currency is the amount prefix shown on the bill, e.g. "Rs ".
	Currency string
}

// Handler returns an http.Handler that serves the station-document upload UI
// and the parse+queue endpoint.
func Handler(cfg Config) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Header().Set("Cache-Control", "no-store")
		w.Write([]byte(stationUI))
	})
	mux.HandleFunc("POST /station/submit", func(w http.ResponseWriter, r *http.Request) {
		handleSubmit(w, r, cfg)
	})
	mux.HandleFunc("GET /station/printers", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, map[string]any{
			"station_printers": cfg.StationPrinters,
			"bill_printer":     cfg.BillPrinter,
			"currency":         cfg.Currency,
		})
	})
	return mux
}

// handleSubmit accepts:
//   - multipart/form-data with field "document" (HTML or .docx file)
//   - application/x-www-form-urlencoded with field "html" (raw HTML paste)
//   - application/json {"html":"…"} or {"text":"…"}
func handleSubmit(w http.ResponseWriter, r *http.Request, cfg Config) {
	ct := r.Header.Get("Content-Type")
	var docText, docKind string
	log.Printf("DEBUG: handleSubmit called, Content-Type: %s", ct)

	switch {
	case strings.HasPrefix(ct, "multipart/form-data"):
		if err := r.ParseMultipartForm(8 << 20); err != nil {
			writeErr(w, http.StatusBadRequest, "cannot parse multipart: "+err.Error())
			return
		}
		f, fh, err := r.FormFile("document")
		if err != nil {
			writeErr(w, http.StatusBadRequest, "field 'document' missing")
			return
		}
		defer f.Close()
		data, _ := io.ReadAll(io.LimitReader(f, 8<<20))
		name := strings.ToLower(fh.Filename)
		switch {
		case strings.HasSuffix(name, ".docx"):
			t, err := extractDocx(data)
			if err != nil {
				writeErr(w, http.StatusBadRequest, "cannot read docx: "+err.Error())
				return
			}
			docText, docKind = t, "text"
		default: // .html, .htm, or anything else treated as HTML
			docText, docKind = string(data), "html"
		}

	case strings.HasPrefix(ct, "application/x-www-form-urlencoded"):
		r.ParseForm()
		if h := r.FormValue("html"); h != "" {
			docText, docKind = h, "html"
		} else if t := r.FormValue("text"); t != "" {
			docText, docKind = t, "text"
		} else {
			writeErr(w, http.StatusBadRequest, "field 'html' or 'text' required")
			return
		}

	case strings.HasPrefix(ct, "application/json"):
		var body struct {
			HTML string `json:"html"`
			Text string `json:"text"`
		}
		if err := json.NewDecoder(io.LimitReader(r.Body, 1<<20)).Decode(&body); err != nil {
			writeErr(w, http.StatusBadRequest, "invalid JSON: "+err.Error())
			return
		}
		log.Printf("DEBUG: JSON body received - html length: %d, text length: %d", len(body.HTML), len(body.Text))
		if body.Text != "" {
			log.Printf("DEBUG: Text content:\n%s", body.Text)
		}
		if body.HTML != "" {
			docText, docKind = body.HTML, "html"
		} else if body.Text != "" {
			docText, docKind = body.Text, "text"
		} else {
			writeErr(w, http.StatusBadRequest, "json field 'html' or 'text' required")
			return
		}

	default:
		writeErr(w, http.StatusUnsupportedMediaType, "send multipart/form-data, application/json or application/x-www-form-urlencoded")
		return
	}

	// Parse.
	var (
		order *Order
		err   error
	)
	if docKind == "html" {
		order, err = ParseHTML(docText, cfg.StationPrinters)
	} else {
		order, err = ParseText(docText, cfg.StationPrinters)
	}
	if err != nil {
		writeErr(w, http.StatusUnprocessableEntity, err.Error())
		return
	}

	// Apply defaults.
	if order.Restaurant == "" {
		order.Restaurant = "Palate Kitchen"
	}
	if order.OrderNumber == "" {
		order.OrderNumber = fmt.Sprintf("%d", time.Now().Unix()%100000)
	}
	billPrinter := order.BillPrinter
	if billPrinter == "" {
		billPrinter = cfg.BillPrinter
	}
	currency := cfg.Currency
	if currency == "" {
		currency = "Rs "
	}

	// Split into per-station KOT jobs.
	type station struct{ items []api.Item }
	byStation := map[string]*station{} // printer id -> items
	stLabel   := map[string]string{}   // printer id -> station label
	var billItems []api.BillItem
	var subtotal int
	for _, it := range order.Items {
		if it.PrinterID == "" {
			// No printer mapping: skip from KOT routing but keep in bill.
		} else {
			if byStation[it.PrinterID] == nil {
				byStation[it.PrinterID] = &station{}
				stLabel[it.PrinterID] = it.Station
			}
			byStation[it.PrinterID].items = append(byStation[it.PrinterID].items, api.Item{
				Quantity: it.Quantity, Name: it.Name, Notes: it.Notes,
			})
		}
		amt := it.Quantity * it.UnitPrice
		subtotal += amt
		billItems = append(billItems, api.BillItem{
			Quantity: it.Quantity, Name: it.Name, Notes: it.Notes,
			UnitPrice: it.UnitPrice, Amount: amt,
		})
	}

	// Queue KOTs.
	var ids []string
	for pid, st := range byStation {
		slip := api.Slip{
			Station:     stLabel[pid],
			Table:       order.Table,
			OrderNumber: order.OrderNumber,
			PlacedAt:    order.PlacedAt,
			Items:       st.items,
		}
		ids = append(ids, cfg.MockServer.NewJob(cfg.Restaurant, pid, slip))
	}

	// Queue bill.
	var billID string
	if billPrinter != "" && len(billItems) > 0 {
		tax  := subtotal * order.TaxRate / 10000
		svc  := order.ServiceCharge
		total := subtotal + tax + svc
		bill := api.Bill{
			Restaurant:    order.Restaurant,
			Title:         "CUSTOMER BILL",
			BillNumber:    order.OrderNumber,
			Table:         order.Table,
			OrderNumber:   order.OrderNumber,
			PlacedAt:      order.PlacedAt,
			Items:         billItems,
			Subtotal:      subtotal,
			TaxRate:       order.TaxRate,
			Tax:           tax,
			ServiceCharge: svc,
			Total:         total,
			Currency:      currency,
			Footer:        order.Footer,
		}
		billID = cfg.MockServer.NewBill(cfg.Restaurant, billPrinter, bill)
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"order_number": order.OrderNumber,
		"table":        order.Table,
		"stations":     len(byStation),
		"items":        len(order.Items),
		"kot_ids":      ids,
		"bill_id":      billID,
	})
}

// extractDocx pulls the plain text from a Word .docx file (which is a ZIP
// containing word/document.xml).
func extractDocx(data []byte) (string, error) {
	r, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return "", err
	}
	for _, f := range r.File {
		if f.Name != "word/document.xml" {
			continue
		}
		rc, err := f.Open()
		if err != nil {
			return "", err
		}
		defer rc.Close()
		xmlBytes, _ := io.ReadAll(io.LimitReader(rc, 4<<20))
		// Strip XML tags; paragraph ends (<w:p>) become newlines.
		text := regexp.MustCompile(`<w:p[ />]`).ReplaceAllString(string(xmlBytes), "\n<w:p")
		text = regexp.MustCompile(`<[^>]+>`).ReplaceAllString(text, " ")
		return text, nil
	}
	return "", fmt.Errorf("word/document.xml not found in docx")
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(v)
}

func writeErr(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]string{"error": msg})
}

// stationUI is the minimal HTML page served at /.
// Single form with all stations, one button to send everything.
const stationUI = `<!doctype html>
<html lang="en">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width,initial-scale=1">
<title>Palate order entry</title>
<style>
  *{box-sizing:border-box}
  body{margin:0;font:14px/1.5 "Segoe UI",system-ui,sans-serif;background:#f4f3ef;color:#1f1e1c}
  header{background:#fff;border-bottom:1px solid #e3e0d8;padding:12px 24px}
  header h1{margin:0;font:400 20px Georgia,serif}header h1 b{color:#3b67b0;font-weight:400}
  .wrap{max-width:900px;margin:24px auto;padding:0 20px}
  .card{background:#fff;border:1px solid #e3e0d8;border-radius:8px;padding:20px;margin-bottom:16px}
  h2{font:600 15px "Segoe UI",sans-serif;margin:0 0 8px;text-transform:uppercase;letter-spacing:.04em;color:#3b67b0}
  h3{font:600 13px "Segoe UI",sans-serif;margin:16px 0 8px;color:#6b6862}
  .stations{display:grid;grid-template-columns:repeat(3,1fr);gap:16px;margin-bottom:20px}
  .station{border:1px solid #e3e0d8;border-radius:6px;padding:12px}
  .station.tandoor{border-top:3px solid #d97706}.station.pass{border-top:3px solid #059669}
  .station.wok{border-top:3px solid #dc2626}
  .station h4{margin:0 0 8px;font:600 12px "Segoe UI",sans-serif;text-transform:uppercase;letter-spacing:.04em}
  .station.tandoor h4{color:#d97706}.station.pass h4{color:#059669}.station.wok h4{color:#dc2626}
  label{font-size:12px;color:#6b6862;display:block;margin:8px 0 3px}
  input[type=text],textarea{font:inherit;font-size:13px;border:1px solid #d3cfc5;
    border-radius:4px;padding:6px 8px;background:#fff;width:100%}
  input[type=text]:focus,textarea:focus{outline:none;border-color:#3b67b0}
  textarea{height:100px;font-family:Consolas,"Cascadia Mono",monospace;font-size:12px;resize:vertical}
  button{font:600 14px "Segoe UI",sans-serif;border:none;background:#3b67b0;color:#fff;
    border-radius:6px;padding:12px 24px;cursor:pointer;width:100%;font-size:15px}
  button:hover{background:#315a9c}
  .hint{font-size:11px;color:#9b9893;margin-bottom:6px;line-height:1.4}
  .result{background:#f6f5f1;border:1px solid #e3e0d8;border-radius:5px;padding:12px;
    font-size:11px;white-space:pre-wrap;word-break:break-word;max-height:200px;overflow:auto;margin-top:12px}
  .ok{color:#1d7a45}.err{color:#b0362a}
</style>
</head>
<body>
<header>
  <h1><b>Palate</b> order entry</h1>
</header>
<div class="wrap">

  <div class="card">
    <h2>New Order</h2>
    <form id="orderForm">
      <label>Order Number / Table</label>
      <input type="text" id="orderNumber" placeholder="1042 / T4" required>
      
      <h3>Station Items</h3>
      <div class="stations">
        
        <div class="station tandoor">
          <h4>🔥 Tandoor</h4>
          <p class="hint">One item per line: 2x Paneer : 240</p>
          <textarea id="tandoorItems" placeholder="2x Paneer Tikka : 240
4x Butter Naan : 60"></textarea>
        </div>

        <div class="station pass">
          <h4>🍹 Pass</h4>
          <p class="hint">One item per line: 2x Chai : 40</p>
          <textarea id="passItems" placeholder="2x Masala Chai : 40
1x Fresh Lime Soda : 50"></textarea>
        </div>

        <div class="station wok">
          <h4>🥘 Wok</h4>
          <p class="hint">One item per line: 1x Noodles : 180</p>
          <textarea id="wokItems" placeholder="1x Veg Hakka Noodles : 180
2x Veg Manchurian : 160"></textarea>
        </div>

      </div>

      <button type="submit">🖨️ Send Order to All Stations + Print Bill</button>
      <div class="result" id="result" style="display:none"></div>
    </form>
  </div>

</div>
<script>
"use strict";

document.getElementById("orderForm").addEventListener("submit", async (e) => {
  e.preventDefault();
  
  const orderNumber = document.getElementById("orderNumber").value.trim();
  const tandoorItems = document.getElementById("tandoorItems").value.trim();
  const passItems = document.getElementById("passItems").value.trim();
  const wokItems = document.getElementById("wokItems").value.trim();
  
  if (!orderNumber) {
    alert("Enter Order Number / Table");
    return;
  }
  
  if (!tandoorItems && !passItems && !wokItems) {
    alert("Enter items for at least one station");
    return;
  }
  
  const orderParts = orderNumber.split("/");
  const order = orderParts[0].trim();
  const table = orderParts.length > 1 ? orderParts[1].trim() : "";
  
  // Build combined text with all items
  let allText = "";
  
  const processItems = (items, station) => {
    items.split("\n").forEach(line => {
      line = line.trim();
      if (!line) return;
      console.log("Processing line:", line, "for station:", station);
      // Parse format: 2x Item : 240
      // Split by colon to extract item and price
      const parts = line.split(":").map(p => p.trim());
      console.log("Parts:", parts);
      if (parts.length === 2) {
        // Add station name between item and price
        const formatted = parts[0] + " : " + station + " : " + parts[1];
        console.log("Formatted:", formatted);
        allText += formatted + "\n";
      } else {
        // No price, just add station
        const formatted = line + " : " + station;
        console.log("Formatted (no price):", formatted);
        allText += formatted + "\n";
      }
    });
  };
  
  if (tandoorItems) processItems(tandoorItems, "Tandoor");
  if (passItems) processItems(passItems, "Pass");
  if (wokItems) processItems(wokItems, "Wok");
  
  allText += "Order: " + order + "\n";
  if (table) allText += "Table: " + table + "\n";
  allText += "Tax: 5%\n";
  allText += "Bill-Printer: p-bill\n";
  
  console.log("Final text to submit:");
  console.log(allText);
  
  const resultDiv = document.getElementById("result");
  try {
    const r = await fetch("/station/submit", {
      method:"POST",
      headers:{"Content-Type":"application/json"},
      body: JSON.stringify({ text: allText })
    });
    const d = await r.json();
    resultDiv.style.display = "";
    resultDiv.className = "result " + (r.ok ? "ok" : "err");
    if (r.ok) {
      resultDiv.textContent = "✓ Order sent successfully!\n" +
        "KOTs: " + (d.kot_ids?.length || 0) + " station(s)\n" +
        "Bill: " + (d.bill_id || "none") + "\n" +
        "Order: " + d.order_number + " | Table: " + (d.table || "—");
      // Clear form on success
      document.getElementById("tandoorItems").value = "";
      document.getElementById("passItems").value = "";
      document.getElementById("wokItems").value = "";
      document.getElementById("orderNumber").value = "";
    } else {
      resultDiv.textContent = JSON.stringify(d, null, 2);
    }
  } catch(err) {
    resultDiv.style.display = "";
    resultDiv.className = "result err";
    resultDiv.textContent = "Error: " + err.message;
  }
});
</script>
</body>
</html>
`
