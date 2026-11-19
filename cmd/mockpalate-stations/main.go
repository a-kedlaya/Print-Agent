// Command mockpalate-stations is a mock Palate server for testing the print
// agent with real station documents (HTML or .docx).
//
// It embeds the existing mock Palate server (agent polling, leasing, outcomes)
// and adds a station-document upload layer on a second port: you POST an HTML
// or docx order document, it is parsed into per-station kitchen ticket jobs
// and one combined customer bill, and those are queued into the Palate server
// for the agent to pick up and print.
//
//	mockpalate-stations
//	mockpalate-stations -station-port 8081 -agent-port 8080 -seed demo
//	mockpalate-stations -stations "tandoor=p-tandoor,wok=p-wok,pass=p-pass,bill=p-bill"
//
// The monitoring dashboard (read-only, no order entry) is at the agent-port.
// The station-document upload UI is at the station-port.
//
// Station-to-printer mapping (-stations flag):
//
//	comma-separated   station_name=printer_id pairs (case-insensitive station names)
//
// Bill printer (-bill-printer flag): printer ID for the combined customer bill.
// Currency prefix (-currency flag): e.g. "Rs " (default).
// Tax rate (-tax flag): percentage, e.g. 5 (default 0).
//
// Use -printer-control to connect the monitoring page to fake printer control APIs.
package main

import (
	"flag"
	"fmt"
	"log"
	"net/http"
	"net/netip"
	"net/url"
	"strings"
	"time"

	"github.com/ShreyasBairyKS/Palate-Print-Agent/internal/mockserver"
	"github.com/ShreyasBairyKS/Palate-Print-Agent/stationserver"
)

func main() {
	agentPort   := flag.String("agent-port",   "8080", "port for the monitoring page and agent long-poll")
	stationPort := flag.String("station-port", "8081", "port for the station-document upload UI")
	hold        := flag.Duration("hold",   25*time.Second, "how long a poll is held with no jobs")
	lease       := flag.Duration("lease",  60*time.Second, "how long a fetched job is leased")
	seed        := flag.String("seed",    "", "print a pairing code for this restaurant id at startup")
	stations    := flag.String("stations", "tandoor=p-tandoor,wok=p-wok,pass=p-pass",
		"comma-separated station=printer_id pairs (case-insensitive station names)")
	billPrinter := flag.String("bill-printer", "p-bill",  "printer id for the combined customer bill")
	currency    := flag.String("currency",    "Rs ",     "currency prefix on the bill (e.g. \"Rs \")")
	restaurant  := flag.String("restaurant",  "demo",    "restaurant id used for all queued jobs")
	printerCtrl := flag.String("printer-control", "",
		"demo page: comma-separated id=http://127.0.0.1:port pairs for fake printer control APIs")
	flag.Parse()

	// Parse station mapping.
	sp := parseStations(*stations)

	// Parse printer control map.
	controls, err := parseControls(*printerCtrl)
	if err != nil {
		log.Fatal(err)
	}

	// Start the mock Palate server (agent polling layer).
	srv := mockserver.New(mockserver.Config{
		Hold:           *hold,
		Lease:          *lease,
		PrinterControl: controls,
	})
	defer srv.Close()

	if *seed != "" {
		code, exp := srv.NewPairingCode(*seed)
		log.Printf("pairing code for restaurant %q: %s (expires %s)", *seed, code, exp.Format(time.Kitchen))
	}

	// Agent-facing + monitoring dashboard on agentPort.
	agentAddr := "127.0.0.1:" + *agentPort
	agentHS := &http.Server{
		Addr:              agentAddr,
		Handler:           logRequests(srv),
		ReadHeaderTimeout: 5 * time.Second,
	}
	go func() { log.Fatal(agentHS.ListenAndServe()) }()
	log.Printf("mock Palate server (agent + monitor) on http://%s", agentAddr)
	log.Printf("monitoring dashboard: http://%s/", agentAddr)

	// Station-document upload UI on stationPort.
	stationAddr := "127.0.0.1:" + *stationPort
	stationHandler := stationserver.Handler(stationserver.Config{
		MockServer:      srv,
		Restaurant:      *restaurant,
		StationPrinters: sp,
		BillPrinter:     *billPrinter,
		Currency:        *currency,
	})
	stationHS := &http.Server{
		Addr:              stationAddr,
		Handler:           logRequests(stationHandler),
		ReadHeaderTimeout: 5 * time.Second,
	}
	go func() { log.Fatal(stationHS.ListenAndServe()) }()
	log.Printf("station-document upload UI on http://%s/", stationAddr)
	log.Printf("station→printer map: %v", sp)
	log.Printf("bill printer: %s  currency: %s", *billPrinter, *currency)

	// Block forever (both servers run in goroutines).
	select {}
}

// parseStations parses "tandoor=p-tandoor,wok=p-wok,pass=p-pass".
func parseStations(s string) map[string]string {
	out := map[string]string{}
	for _, part := range strings.Split(s, ",") {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		k, v, ok := strings.Cut(part, "=")
		if !ok || k == "" || v == "" {
			log.Printf("ignoring bad station mapping %q (want station=printer_id)", part)
			continue
		}
		out[strings.ToLower(strings.TrimSpace(k))] = strings.TrimSpace(v)
	}
	return out
}

// parseControls reads "id=http://127.0.0.1:port" pairs (loopback only).
func parseControls(s string) (map[string]string, error) {
	out := map[string]string{}
	for _, part := range strings.Split(s, ",") {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		id, raw, ok := strings.Cut(part, "=")
		u, err := url.Parse(raw)
		if !ok || id == "" || err != nil || u.Scheme != "http" {
			return nil, fmt.Errorf("-printer-control: %q is not id=http://127.0.0.1:port", part)
		}
		if ip, err := netip.ParseAddr(u.Hostname()); err != nil || !ip.IsLoopback() {
			return nil, fmt.Errorf("-printer-control: %q must point at a loopback address", part)
		}
		out[id] = raw
	}
	return out, nil
}

type statusRecorder struct {
	http.ResponseWriter
	status int
}

func (r *statusRecorder) WriteHeader(code int) {
	r.status = code
	r.ResponseWriter.WriteHeader(code)
}

func (r *statusRecorder) Unwrap() http.ResponseWriter { return r.ResponseWriter }

func logRequests(h http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		rec := &statusRecorder{ResponseWriter: w, status: 200}
		h.ServeHTTP(rec, r)
		log.Printf("%s %s -> %d (%s)", r.Method, r.URL.Path, rec.status, time.Since(start).Round(time.Millisecond))
	})
}
