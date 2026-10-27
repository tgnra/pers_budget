package main

import (
	"encoding/json"
	"testing"
)

func d(s string) Date {
	v, err := ParseDate(s)
	if err != nil {
		panic(err)
	}
	return v
}

func m(s string) Money {
	v, err := ParseMoney(s)
	if err != nil {
		panic(err)
	}
	return v
}

func prio(n int) *int { return &n }

func dates(ds []Date) []string {
	var out []string
	for _, x := range ds {
		out = append(out, x.String())
	}
	return out
}

func eqStrings(t *testing.T, got, want []string) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("got %v, want %v", got, want)
	}
	for i := range got {
		if got[i] != want[i] {
			t.Fatalf("got %v, want %v", got, want)
		}
	}
}

func TestParseMoney(t *testing.T) {
	for in, want := range map[string]Money{
		"12": 1200, "12.3": 1230, "-12.34": -1234, "1,234.50": 123450, "$5": 500, "-$5.01": -501, ".5": 50, "+3": 300,
	} {
		if got, err := ParseMoney(in); err != nil || got != want {
			t.Errorf("ParseMoney(%q) = %v, %v; want %v", in, got, err, want)
		}
	}
	for _, in := range []string{"", "-", "1.234", "abc", "1.-5", "--1"} {
		if _, err := ParseMoney(in); err == nil {
			t.Errorf("ParseMoney(%q) should fail", in)
		}
	}
	if s := Money(-123456789).String(); s != "-1,234,567.89" {
		t.Errorf("String = %s", s)
	}
	if s := Money(-5).Plain(); s != "-0.05" {
		t.Errorf("Plain = %s", s)
	}
}

func TestMonthlyClampsToMonthEnd(t *testing.T) {
	r := Recurring{Freq: Monthly, Interval: 1, Start: d("2026-01-31")}
	eqStrings(t, dates(r.Dates(d("2026-05-01"))),
		[]string{"2026-01-31", "2026-02-28", "2026-03-31", "2026-04-30"})
}

func TestMonthlyLastDay(t *testing.T) {
	r := Recurring{Freq: MonthlyLast, Interval: 1, Start: d("2027-01-15")}
	eqStrings(t, dates(r.Dates(d("2027-04-30"))),
		[]string{"2027-01-31", "2027-02-28", "2027-03-31", "2027-04-30"})
	r = Recurring{Freq: MonthlyLast, Interval: 2, Start: d("2027-12-01")}
	eqStrings(t, dates(r.Dates(d("2028-04-30"))), []string{"2027-12-31", "2028-02-29", "2028-04-30"})
}

func TestWeeklyWithIntervalAndEnd(t *testing.T) {
	end := d("2026-11-24")
	r := Recurring{Freq: Weekly, Interval: 2, Start: d("2026-10-27"), End: &end}
	eqStrings(t, dates(r.Dates(d("2027-01-01"))), []string{"2026-10-27", "2026-11-10", "2026-11-24"})
}

func TestPriorityOrdersSameDay(t *testing.T) {
	b := &Budget{}
	b.AddTransaction(d("2026-11-01"), "one-off", m("-5"), false)
	err := b.SetRecurring([]Recurring{
		{Desc: "rent", Amount: m("-1000"), Freq: Monthly, Interval: 1, Start: d("2026-11-01"), Priority: prio(2)},
		{Desc: "pay", Amount: m("2000"), Freq: Monthly, Interval: 1, Start: d("2026-11-01"), Priority: prio(1)},
		{Desc: "misc", Amount: m("-1"), Freq: Monthly, Interval: 1, Start: d("2026-11-01")},
	})
	if err != nil {
		t.Fatal(err)
	}
	b.Reconcile(d("2026-11-01"), m("100"))
	var got []string
	for _, e := range b.Ledger(d("2026-11-01")) {
		got = append(got, e.Desc)
	}
	eqStrings(t, got, []string{"pay", "rent", "one-off", "misc", "Reconcile"})
}

func TestOccurrenceEditsAndProjection(t *testing.T) {
	b := &Budget{}
	b.SetRecurring([]Recurring{
		{Desc: "pay", Amount: m("1000"), Freq: Monthly, Interval: 1, Start: d("2026-01-15")},
		{Desc: "rent", Amount: m("-800"), Freq: MonthlyLast, Interval: 1, Start: d("2026-01-01")},
	})
	pay, rent := b.Recurring[0].ID, b.Recurring[1].ID
	today := d("2026-10-27")

	// Bank says 500 today.
	b.Reconcile(today, m("500"))
	if got := b.BalanceOn(today); got != m("500") {
		t.Fatalf("balance after reconcile = %v", got)
	}

	// Rent on Oct 31 is 900 this month; Nov pay is skipped.
	if err := b.EditOccurrence(rent, d("2026-10-31"), d("2026-10-31"), m("-900"), "rent"); err != nil {
		t.Fatal(err)
	}
	if err := b.SkipOccurrence(pay, d("2026-11-15")); err != nil {
		t.Fatal(err)
	}
	if err := b.EditOccurrence(pay, d("2026-12-15"), d("2026-12-15"), m("1000"), "pay"); err != nil {
		t.Fatal(err)
	}
	if len(b.Overrides) != 2 {
		t.Fatalf("an edit equal to the series should not be stored; overrides = %+v", b.Overrides)
	}
	if err := b.EditOccurrence(pay, d("2026-11-16"), d("2026-11-16"), m("1"), "x"); err == nil {
		t.Fatal("editing an unscheduled date should fail")
	}

	p := b.Project(today, today, d("2026-12-31"))
	// 500 -900 (Oct 31) = -400; Nov 15 skipped; -800 (Nov 30) = -1200; +1000 = -200; -800 = -1000
	if p.Lowest != m("-1200") || p.LowestDate != d("2026-11-30") {
		t.Fatalf("lowest = %v on %v", p.Lowest, p.LowestDate)
	}
	if p.TodayBalance != m("500") {
		t.Fatalf("today = %v", p.TodayBalance)
	}

	// Removing the pay series drops its overrides.
	b.SetRecurring(b.Recurring[1:])
	for _, ov := range b.Overrides {
		if ov.RecID == pay {
			t.Fatal("override for removed series kept")
		}
	}
}

func TestMovedOccurrence(t *testing.T) {
	b := &Budget{}
	b.SetRecurring([]Recurring{{Desc: "bill", Amount: m("-10"), Freq: Monthly, Interval: 1, Start: d("2026-01-10")}})
	id := b.Recurring[0].ID
	// Move Feb 10 back to Jan 31: it should appear in a ledger through Jan 31.
	b.EditOccurrence(id, d("2026-02-10"), d("2026-01-31"), m("-10"), "bill")
	if got := b.BalanceOn(d("2026-01-31")); got != m("-20") {
		t.Fatalf("balance = %v", got)
	}
	if got := b.BalanceOn(d("2026-02-28")); got != m("-20") {
		t.Fatalf("balance = %v (moved occurrence counted twice?)", got)
	}
}

func TestJSONRoundTrip(t *testing.T) {
	b := &Budget{}
	end := d("2027-06-30")
	b.SetRecurring([]Recurring{{Desc: "x", Amount: m("-1.5"), Freq: MonthlyLast, Interval: 1, Start: d("2026-01-01"), End: &end, Priority: prio(3)}})
	b.AddTransaction(d("2026-03-03"), "y", m("2"), false)
	b.SkipOccurrence(b.Recurring[0].ID, d("2026-02-28"))
	data, err := json.Marshal(b)
	if err != nil {
		t.Fatal(err)
	}
	var c Budget
	if err := json.Unmarshal(data, &c); err != nil {
		t.Fatal(err)
	}
	again, _ := json.Marshal(&c)
	if string(again) != string(data) {
		t.Fatalf("round trip mismatch:\n%s\n%s", data, again)
	}
}
