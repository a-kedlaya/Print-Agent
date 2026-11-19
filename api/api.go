// Package api is the wire format between the agent and Palate's server.
//
// PROVISIONAL. These paths and shapes are a working design until the backend
// API is specified; the specification wins on any difference. Reconciling
// should only touch this package, source/cloud, pair/redeem.go and the mock
// server.
package api

import "time"

// Paths. All but Pair require "Authorization: Bearer <device credential>".
const (
	PathPair     = "/v1/pair"              // POST  PairRequest -> PairResponse
	PathPrinters = "/v1/printers"          // PUT   PrintersRequest -> 204
	PathPoll     = "/v1/jobs/poll"         // POST  PollRequest -> PollResponse (long poll)
	PathOutcome  = "/v1/jobs/{id}/outcome" // POST  OutcomeRequest -> 204
)

// MaxResponseBytes is the most the agent will read from any response. A
// larger body is an attack or a bug; the agent must refuse it.
const MaxResponseBytes = 1 << 20 // 1 MiB

// PairRequest redeems a single-use pairing code.
type PairRequest struct {
	Code      string `json:"code"`
	AgentName string `json:"agent_name"` // e.g. the till's hostname, for the portal
}

type PairResponse struct {
	DeviceID   string `json:"device_id"`
	Credential string `json:"credential"`
}

// Printer is one printer the agent can see. Addresses are never sent: the
// server knows printers only by id and name.
type Printer struct {
	ID    string `json:"id"`
	Name  string `json:"name"`  // "Tandoor"
	Kind  string `json:"kind"`  // "network" or "usb"
	Width int    `json:"width"` // 58 or 80
}

type PrintersRequest struct {
	Printers []Printer `json:"printers"`
}

// PollRequest carries the agent's health on every poll. No customer data.
type PollRequest struct {
	Health Health `json:"health"`
}

type Health struct {
	AgentVersion string          `json:"agent_version"`
	QueueDepth   int             `json:"queue_depth"`
	LastError    string          `json:"last_error,omitempty"` // an error code, never slip contents
	Printers     []PrinterHealth `json:"printers"`
	BadVersion   string          `json:"bad_version,omitempty"` // an update that was reverted
	// WatchdogRestarts counts poll loops the watchdog had to restart.
	WatchdogRestarts int `json:"watchdog_restarts,omitempty"`
}

type PrinterHealth struct {
	ID        string `json:"id"`
	State     string `json:"state"` // "up", "down", "unknown"
	LastError string `json:"last_error,omitempty"`
}

// PollResponse is returned as soon as a job exists, or with no jobs once the
// server's hold time passes.
//
// Compatibility: the agent ignores fields it doesn't know, at every level, so
// the server can add fields without breaking agents already installed. The
// exceptions are fields that try to steer printing (see source/cloud's
// forbidden list): a job carrying one is refused. Information that must
// reach the paper goes in Slip.Extras.
type PollResponse struct {
	Jobs         []Job `json:"jobs"`
	LeaseSeconds int   `json:"lease_seconds"`
	// NewCredential is set when the server rotates the credential. The agent
	// stores it before using it; the old one stays valid until first use.
	NewCredential string `json:"new_credential,omitempty"`
}

type Job struct {
	ID        string    `json:"id"`
	PrinterID string    `json:"printer_id"`
	CreatedAt time.Time `json:"created_at"`
	Slip      Slip      `json:"slip"`
	// Bill is set instead of Slip when this job is the customer bill.
	// Exactly one of Slip or Bill is populated; the other is zero.
	// A Slip is a kitchen order ticket (no prices); a Bill carries prices
	// and totals for the customer. Both are routed to their printer_id.
	Bill *Bill `json:"bill,omitempty"`
}

// Bill is the customer-facing bill: every station's items for one order
// combined, with prices and totals. It is separate from Slip, which is a
// kitchen order ticket and carries no prices by design (DPDP Act 2023).
// Money is in paise (minor units) everywhere, so totals never drift through
// floating point. TaxRate is in basis points (500 = 5.00%).
type Bill struct {
	Title         string     `json:"title,omitempty"`      // "CUSTOMER BILL"; defaults to that
	Restaurant    string     `json:"restaurant,omitempty"` // printed header
	BillNumber    string     `json:"bill_number,omitempty"`
	Table         string     `json:"table,omitempty"`
	OrderNumber   string     `json:"order_number,omitempty"`
	Server        string     `json:"server,omitempty"`
	Guests        int        `json:"guests,omitempty"`
	PlacedAt      time.Time  `json:"placed_at"`
	Items         []BillItem `json:"items"`
	Subtotal      int        `json:"subtotal"`                 // paise
	TaxRate       int        `json:"tax_rate,omitempty"`       // basis points (500 = 5.00%)
	Tax           int        `json:"tax,omitempty"`            // paise
	ServiceCharge int        `json:"service_charge,omitempty"` // paise
	Discount      int        `json:"discount,omitempty"`       // paise
	Total         int        `json:"total"`                    // paise
	Payments      []Payment  `json:"payments,omitempty"`
	Change        int        `json:"change,omitempty"`   // paise
	Currency      string     `json:"currency,omitempty"` // amount prefix, e.g. "Rs "
	Footer        string     `json:"footer,omitempty"`   // e.g. a GSTIN line
	Duplicate     bool       `json:"duplicate,omitempty"`
	MovedFrom     string     `json:"moved_from,omitempty"`
}

// BillItem is one line on the bill, with its price.
type BillItem struct {
	Quantity  int    `json:"quantity"`
	Name      string `json:"name"`
	Notes     string `json:"notes,omitempty"`
	UnitPrice int    `json:"unit_price"` // paise
	Amount    int    `json:"amount"`     // paise (Quantity * UnitPrice)
}

// Payment is one tendered amount on the bill.
type Payment struct {
	Method string `json:"method"` // "Cash", "Card"
	Amount int    `json:"amount"` // paise
}

type Slip struct {
	Station     string    `json:"station"`
	Table       string    `json:"table,omitempty"`
	OrderNumber string    `json:"order_number"`
	PlacedAt    time.Time `json:"placed_at"`
	Items       []Item    `json:"items"`
	Notes       string    `json:"notes,omitempty"`
	Duplicate   bool      `json:"duplicate,omitempty"`
	MovedFrom   string    `json:"moved_from,omitempty"`
	// Extras are labelled lines printed under the order line, such as
	// {"label": "Course", "value": "2"}. New information the server wants on
	// the ticket goes here, so agents already in restaurants print it without
	// an update. The agent prints at most MaxExtras.
	Extras []Extra `json:"extras,omitempty"`
}

// MaxExtras is the most extra lines the agent prints on one ticket.
const MaxExtras = 10

// Extra is one labelled line on the ticket.
type Extra struct {
	Label string `json:"label"`
	Value string `json:"value"`
}

type Item struct {
	Quantity int    `json:"quantity"`
	Name     string `json:"name"`
	Notes    string `json:"notes,omitempty"`
}

type OutcomeRequest struct {
	Kind      string `json:"kind"` // printed, written, fallback, failed, screen_only
	PrinterID string `json:"printer_id,omitempty"`
	Reason    string `json:"reason,omitempty"`
}

// Error is the body of any non-2xx response.
type Error struct {
	Code    string `json:"code"` // "unauthorized", "repair_needed", "not_found", ...
	Message string `json:"message"`
}
