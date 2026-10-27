package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"sort"
	"strconv"
	"strings"
	"time"
)

/*
Personal budgeting with running-balance projection.

Conventions:
  - Positive amounts are income, negative amounts are expenses.
  - The running balance = every transaction (past AND future) up to a date.
    Past transactions therefore always feed into any projection; reconciling
    adds a one-off adjustment that brings today's balance in line with the bank.
  - Recurring transactions are expanded on demand, so editing a single occurrence
    never touches the rest of the series.
  - Same-day entries are ordered by recurring priority (1 first), then entries
    without a priority in insertion order, then reconciliation adjustments.
*/

// ---------------------------------------------------------------- Date

// Date is a calendar date, stored as days since 1970-01-01.
type Date int

func DateOf(t time.Time) Date {
	y, m, d := t.Date()
	return Date(time.Date(y, m, d, 0, 0, 0, 0, time.UTC).Unix() / 86400)
}

func NewDate(y int, m time.Month, d int) Date {
	return DateOf(time.Date(y, m, d, 0, 0, 0, 0, time.UTC))
}

func ParseDate(s string) (Date, error) {
	t, err := time.Parse("2006-01-02", strings.TrimSpace(s))
	if err != nil {
		return 0, fmt.Errorf("invalid date %q", s)
	}
	return DateOf(t), nil
}

func (d Date) Time() time.Time { return time.Unix(int64(d)*86400, 0).UTC() }
func (d Date) String() string  { return d.Time().Format("2006-01-02") }

// AddMonths keeps the day-of-month, clamped to month end (Jan 31 + 1 = Feb 28/29).
func (d Date) AddMonths(n int) Date {
	y, m, day := d.Time().Date()
	first := time.Date(y, m+time.Month(n), 1, 0, 0, 0, 0, time.UTC)
	if last := daysIn(first.Year(), first.Month()); day > last {
		day = last
	}
	return NewDate(first.Year(), first.Month(), day)
}

// MonthEnd returns the last day of the month n months after d's month.
func (d Date) MonthEnd(n int) Date {
	y, m, _ := d.Time().Date()
	return DateOf(time.Date(y, m+time.Month(n)+1, 0, 0, 0, 0, 0, time.UTC))
}

func daysIn(y int, m time.Month) int {
	return time.Date(y, m+1, 0, 0, 0, 0, 0, time.UTC).Day()
}

func (d Date) MarshalJSON() ([]byte, error) { return json.Marshal(d.String()) }

func (d *Date) UnmarshalJSON(b []byte) error {
	var s string
	if err := json.Unmarshal(b, &s); err != nil {
		return err
	}
	v, err := ParseDate(s)
	*d = v
	return err
}

// ---------------------------------------------------------------- Money

// Money is an amount in cents.
type Money int64

func ParseMoney(s string) (Money, error) {
	orig := s
	s = strings.ReplaceAll(strings.TrimSpace(s), ",", "")
	neg := false
	if strings.HasPrefix(s, "-") {
		neg, s = true, s[1:]
	} else {
		s = strings.TrimPrefix(s, "+")
	}
	s = strings.TrimPrefix(s, "$")
	whole, frac, _ := strings.Cut(s, ".")
	bad := fmt.Errorf("invalid amount %q", orig)
	if whole == "" && frac == "" || len(frac) > 2 || strings.ContainsAny(whole+frac, "+-") {
		return 0, bad
	}
	if whole == "" {
		whole = "0"
	}
	w, err := strconv.ParseInt(whole, 10, 64)
	if err != nil || w > math.MaxInt64/100-1 {
		return 0, bad
	}
	var f int64
	if frac != "" {
		if f, err = strconv.ParseInt((frac + "0")[:2], 10, 64); err != nil {
			return 0, bad
		}
	}
	v := Money(w*100 + f)
	if neg {
		v = -v
	}
	return v, nil
}

// Plain formats as "-1234.56", suitable for inputs and storage.
func (m Money) Plain() string {
	sign, v := "", int64(m)
	if v < 0 {
		sign, v = "-", -v
	}
	return fmt.Sprintf("%s%d.%02d", sign, v/100, v%100)
}

// String formats with thousands separators: "-1,234.56".
func (m Money) String() string {
	p := m.Plain()
	sign := ""
	if p[0] == '-' {
		sign, p = "-", p[1:]
	}
	whole, frac, _ := strings.Cut(p, ".")
	var b strings.Builder
	for i, c := range whole {
		if i > 0 && (len(whole)-i)%3 == 0 {
			b.WriteByte(',')
		}
		b.WriteRune(c)
	}
	return sign + b.String() + "." + frac
}

func (m Money) MarshalJSON() ([]byte, error) { return json.Marshal(m.Plain()) }

func (m *Money) UnmarshalJSON(b []byte) error {
	var s string
	if err := json.Unmarshal(b, &s); err != nil {
		return err
	}
	v, err := ParseMoney(s)
	*m = v
	return err
}

// ---------------------------------------------------------------- model

const (
	Weekly      = "weekly"
	Monthly     = "monthly"
	MonthlyLast = "monthly-last" // last day of the month
)

// Transaction is a single, non-repeating transaction.
type Transaction struct {
	ID        int    `json:"id"`
	Date      Date   `json:"date"`
	Desc      string `json:"description"`
	Amount    Money  `json:"amount"`
	Reconcile bool   `json:"reconcile,omitempty"`
}

/*
Recurring is a repeating transaction. Interval = 2 with Weekly means "every 2 weeks".
Monthly occurrences keep the start date's day-of-month, clamped to month end
(a series starting Jan 31 lands on Feb 28/29, then Mar 31, and so on).
MonthlyLast occurrences fall on the last day of the month, starting with Start's month.
*/
type Recurring struct {
	ID       int    `json:"id"`
	Desc     string `json:"description"`
	Amount   Money  `json:"amount"`
	Freq     string `json:"frequency"`
	Interval int    `json:"interval"`
	Start    Date   `json:"startDate"`
	End      *Date  `json:"endDate,omitempty"`
	Priority *int   `json:"priority,omitempty"`
}

func (r *Recurring) nth(n int) Date {
	switch r.Freq {
	case Weekly:
		return r.Start + Date(7*n*r.Interval)
	case MonthlyLast:
		return r.Start.MonthEnd(n * r.Interval)
	default:
		return r.Start.AddMonths(n * r.Interval)
	}
}

// Dates returns the originally scheduled dates up to and including upTo (respecting End).
func (r *Recurring) Dates(upTo Date) []Date {
	var out []Date
	for n := 0; ; n++ {
		d := r.nth(n)
		if d > upTo || (r.End != nil && d > *r.End) {
			return out
		}
		out = append(out, d)
	}
}

func (r *Recurring) Scheduled(d Date) bool {
	ds := r.Dates(d)
	return len(ds) > 0 && ds[len(ds)-1] == d
}

func (r *Recurring) Validate() error {
	switch {
	case strings.TrimSpace(r.Desc) == "":
		return errors.New("description is required")
	case r.Freq != Weekly && r.Freq != Monthly && r.Freq != MonthlyLast:
		return fmt.Errorf("unknown frequency %q", r.Freq)
	case r.Interval < 1:
		return errors.New("interval must be at least 1")
	case r.End != nil && *r.End < r.Start:
		return errors.New("end date is before start date")
	case r.Priority != nil && *r.Priority < 1:
		return errors.New("priority must be at least 1")
	}
	return nil
}

// Override holds changes to one occurrence of a recurring transaction. Nil fields = unchanged.
type Override struct {
	RecID   int     `json:"recurringId"`
	Orig    Date    `json:"originalDate"`
	Skipped bool    `json:"skipped,omitempty"`
	Date    *Date   `json:"date,omitempty"`
	Amount  *Money  `json:"amount,omitempty"`
	Desc    *string `json:"description,omitempty"`
}

// Budget is one account's data.
type Budget struct {
	NextID       int           `json:"nextId"`
	Transactions []Transaction `json:"transactions"`
	Recurring    []Recurring   `json:"recurring"`
	Overrides    []Override    `json:"overrides"`
}

// Entry is one line in the ledger, with the running balance after it is applied.
type Entry struct {
	Date      Date
	Desc      string
	Amount    Money
	Balance   Money
	TxID      int  // set for one-offs
	RecID     int  // set for recurring occurrences
	Orig      Date // scheduled date; use this to edit/skip an occurrence
	Edited    bool
	Skipped   bool // shown for reference, not counted in the balance
	Reconcile bool

	rank, seq int
}

// ------------------------------------------------------- one-off items

func (b *Budget) newID() int {
	if b.NextID < 1 {
		b.NextID = 1
	}
	id := b.NextID
	b.NextID++
	return id
}

func (b *Budget) AddTransaction(date Date, desc string, amount Money, reconcile bool) int {
	id := b.newID()
	b.Transactions = append(b.Transactions, Transaction{id, date, desc, amount, reconcile})
	return id
}

func (b *Budget) txIndex(id int) (int, error) {
	for i := range b.Transactions {
		if b.Transactions[i].ID == id {
			return i, nil
		}
	}
	return -1, fmt.Errorf("no one-off transaction with id %d", id)
}

func (b *Budget) UpdateTransaction(id int, date Date, desc string, amount Money) error {
	i, err := b.txIndex(id)
	if err != nil {
		return err
	}
	t := &b.Transactions[i]
	t.Date, t.Desc, t.Amount = date, desc, amount
	return nil
}

func (b *Budget) RemoveTransaction(id int) error {
	i, err := b.txIndex(id)
	if err != nil {
		return err
	}
	b.Transactions = append(b.Transactions[:i], b.Transactions[i+1:]...)
	return nil
}

// ---------------------------------------------------- recurring series

func (b *Budget) recurring(id int) *Recurring {
	for i := range b.Recurring {
		if b.Recurring[i].ID == id {
			return &b.Recurring[i]
		}
	}
	return nil
}

/*
SetRecurring replaces the full list of series. Entries with ID 0 get new IDs.
Overrides for removed series, or for dates a changed series no longer schedules, are dropped.
Note that changing a series changes its past occurrences too, and so today's balance.
*/
func (b *Budget) SetRecurring(list []Recurring) error {
	for i := range list {
		if err := list[i].Validate(); err != nil {
			return fmt.Errorf("%s: %w", list[i].Desc, err)
		}
		if list[i].ID == 0 {
			list[i].ID = b.newID()
		}
	}
	b.Recurring = list
	kept := b.Overrides[:0]
	for _, ov := range b.Overrides {
		if r := b.recurring(ov.RecID); r != nil && r.Scheduled(ov.Orig) {
			kept = append(kept, ov)
		}
	}
	b.Overrides = kept
	return nil
}

// ------------------------------------------ single-occurrence editing

func (b *Budget) checkOccurrence(recID int, orig Date) (*Recurring, error) {
	r := b.recurring(recID)
	if r == nil {
		return nil, fmt.Errorf("no recurring transaction with id %d", recID)
	}
	if !r.Scheduled(orig) {
		return nil, fmt.Errorf("%s is not a scheduled occurrence of %q", orig, r.Desc)
	}
	return r, nil
}

func (b *Budget) setOverride(ov Override) {
	b.RestoreOccurrence(ov.RecID, ov.Orig)
	b.Overrides = append(b.Overrides, ov)
}

/*
EditOccurrence sets one occurrence's values. Fields equal to the series defaults are
stored as "unchanged", and an occurrence matching the series entirely has no override.
*/
func (b *Budget) EditOccurrence(recID int, orig, date Date, amount Money, desc string) error {
	r, err := b.checkOccurrence(recID, orig)
	if err != nil {
		return err
	}
	ov := Override{RecID: recID, Orig: orig}
	if date != orig {
		ov.Date = &date
	}
	if amount != r.Amount {
		ov.Amount = &amount
	}
	if desc != r.Desc {
		ov.Desc = &desc
	}
	if ov.Date == nil && ov.Amount == nil && ov.Desc == nil {
		b.RestoreOccurrence(recID, orig)
	} else {
		b.setOverride(ov)
	}
	return nil
}

// SkipOccurrence removes one occurrence (e.g. a skipped month). Undo with RestoreOccurrence.
func (b *Budget) SkipOccurrence(recID int, orig Date) error {
	if _, err := b.checkOccurrence(recID, orig); err != nil {
		return err
	}
	b.setOverride(Override{RecID: recID, Orig: orig, Skipped: true})
	return nil
}

// RestoreOccurrence discards all edits/skips for an occurrence, returning it to the series default.
func (b *Budget) RestoreOccurrence(recID int, orig Date) {
	kept := b.Overrides[:0]
	for _, ov := range b.Overrides {
		if ov.RecID != recID || ov.Orig != orig {
			kept = append(kept, ov)
		}
	}
	b.Overrides = kept
}

// Reconcile adds an adjustment today so that today's closing balance equals bank.
// It returns the adjustment (zero means nothing was added).
func (b *Budget) Reconcile(today Date, bank Money) Money {
	diff := bank - b.BalanceOn(today)
	if diff != 0 {
		b.AddTransaction(today, "Reconcile", diff, true)
	}
	return diff
}

// -------------------------------------------------------------- queries

const noPriority = math.MaxInt32

// Ledger returns every entry (past and future) up to through, in order, with running balances.
func (b *Budget) Ledger(through Date) []Entry {
	type key struct {
		rec  int
		orig Date
	}
	ovs := make(map[key]*Override, len(b.Overrides))
	for i := range b.Overrides {
		ovs[key{b.Overrides[i].RecID, b.Overrides[i].Orig}] = &b.Overrides[i]
	}

	var raw []Entry
	for _, t := range b.Transactions {
		if t.Date > through {
			continue
		}
		rank := noPriority
		if t.Reconcile {
			rank = noPriority + 1
		}
		raw = append(raw, Entry{Date: t.Date, Desc: t.Desc, Amount: t.Amount, TxID: t.ID,
			Reconcile: t.Reconcile, rank: rank})
	}

	for i := range b.Recurring {
		r := &b.Recurring[i]
		rank := noPriority
		if r.Priority != nil {
			rank = *r.Priority
		}
		emit := func(orig Date) {
			e := Entry{Date: orig, Desc: r.Desc, Amount: r.Amount, RecID: r.ID, Orig: orig, rank: rank}
			if ov := ovs[key{r.ID, orig}]; ov != nil {
				e.Edited, e.Skipped = true, ov.Skipped
				if ov.Date != nil {
					e.Date = *ov.Date
				}
				if ov.Amount != nil {
					e.Amount = *ov.Amount
				}
				if ov.Desc != nil {
					e.Desc = *ov.Desc
				}
			}
			if e.Date <= through {
				raw = append(raw, e)
			}
		}
		// Occurrences whose scheduled date is in range
		for _, d := range r.Dates(through) {
			emit(d)
		}
		// Occurrences scheduled after `through` but moved earlier into range
		for _, ov := range b.Overrides {
			if ov.RecID == r.ID && ov.Orig > through && !ov.Skipped && ov.Date != nil && *ov.Date <= through {
				emit(ov.Orig)
			}
		}
	}

	for i := range raw {
		raw[i].seq = i
	}
	sort.Slice(raw, func(i, j int) bool {
		a, c := raw[i], raw[j]
		if a.Date != c.Date {
			return a.Date < c.Date
		}
		if a.rank != c.rank {
			return a.rank < c.rank
		}
		return a.seq < c.seq
	})

	var bal Money
	for i := range raw {
		if !raw[i].Skipped {
			bal += raw[i].Amount
		}
		raw[i].Balance = bal
	}
	return raw
}

// BalanceOn returns the end-of-day balance on date, counting every transaction on or before it.
func (b *Budget) BalanceOn(date Date) Money {
	l := b.Ledger(date)
	if len(l) == 0 {
		return 0
	}
	return l[len(l)-1].Balance
}

type Projection struct {
	Today, From, To Date
	TodayBalance    Money
	Lowest          Money
	LowestDate      Date
	LowestIdx       int // index into Entries, -1 if the lowest point is today's closing balance
	Entries         []Entry
}

/*
Project lists entries dated from..to. The lowest point is the minimum running balance
after today's close, so dips caused by same-day ordering (see priority) are caught.
*/
func (b *Budget) Project(today, from, to Date) Projection {
	all := b.Ledger(to)
	p := Projection{Today: today, From: from, To: to, LowestIdx: -1}
	for _, e := range all {
		if e.Date <= today {
			p.TodayBalance = e.Balance
		}
	}
	p.Lowest, p.LowestDate = p.TodayBalance, today
	for _, e := range all {
		if e.Date < from {
			continue
		}
		p.Entries = append(p.Entries, e)
		if e.Date > today && !e.Skipped && e.Balance < p.Lowest {
			p.Lowest, p.LowestDate, p.LowestIdx = e.Balance, e.Date, len(p.Entries)-1
		}
	}
	return p
}
