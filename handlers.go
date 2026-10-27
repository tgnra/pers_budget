package main

import (
	"fmt"
	"html/template"
	"io/fs"
	"log"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

type app struct {
	store   *Store
	sess    *sessions
	loc     *time.Location
	pages   map[string]*template.Template
	version string
	icons   map[string][]byte
}

// page is the data passed to every template; D holds page-specific data.
type page struct {
	Title, Page, User, V string
	Msg, Err             string
	D                    any
}

func (a *app) today() Date { return DateOf(time.Now().In(a.loc)) }

func (a *app) routes() http.Handler {
	mux := http.NewServeMux()
	static, _ := fs.Sub(webFS, "web/static")
	mux.Handle("GET /static/", http.StripPrefix("/static/", cacheForever(http.FileServerFS(static))))
	mux.HandleFunc("GET /manifest.webmanifest", a.serveFile("web/static/manifest.webmanifest", "application/manifest+json"))
	mux.HandleFunc("GET /sw.js", a.serveFile("web/static/sw.js", "text/javascript"))
	for name := range a.icons {
		mux.HandleFunc("GET /"+name, a.serveIcon(name))
	}

	mux.HandleFunc("GET /login", a.loginPage)
	mux.HandleFunc("POST /login", a.login)
	mux.HandleFunc("POST /logout", a.logout)

	mux.HandleFunc("GET /{$}", a.auth(a.mainPage))
	mux.HandleFunc("POST /entry", a.auth(a.editEntry))
	mux.HandleFunc("POST /reconcile", a.auth(a.reconcile))
	mux.HandleFunc("GET /recurring", a.auth(a.recurringPage))
	mux.HandleFunc("POST /recurring", a.auth(a.saveRecurring))
	return mux
}

func cacheForever(h http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("v") != "" {
			w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
		}
		h.ServeHTTP(w, r)
	})
}

func (a *app) serveFile(name, ctype string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		b, _ := webFS.ReadFile(name)
		w.Header().Set("Content-Type", ctype)
		w.Header().Set("Cache-Control", "no-cache")
		w.Write(b)
	}
}

func (a *app) serveIcon(name string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "image/png")
		w.Header().Set("Cache-Control", "public, max-age=86400")
		w.Write(a.icons[name])
	}
}

func (a *app) render(w http.ResponseWriter, name string, p page) {
	p.V = a.version
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	if err := a.pages[name].ExecuteTemplate(w, "layout", p); err != nil {
		log.Printf("render %s: %v", name, err)
	}
}

func (a *app) auth(h func(http.ResponseWriter, *http.Request, string)) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		user := a.sess.user(r)
		if user == "" {
			http.Redirect(w, r, "/login", http.StatusSeeOther)
			return
		}
		h(w, r, user)
	}
}

func (a *app) fail(w http.ResponseWriter, err error) {
	log.Print(err)
	http.Error(w, "internal error", http.StatusInternalServerError)
}

// redirect sends the browser to target with an optional flash message.
func redirect(w http.ResponseWriter, r *http.Request, target, msg string) {
	if msg != "" {
		sep := "?"
		if strings.Contains(target, "?") {
			sep = "&"
		}
		target += sep + "msg=" + url.QueryEscape(msg)
	}
	http.Redirect(w, r, target, http.StatusSeeOther)
}

// ---------------------------------------------------------------- login

func (a *app) loginPage(w http.ResponseWriter, r *http.Request) {
	if a.sess.user(r) != "" {
		http.Redirect(w, r, "/", http.StatusSeeOther)
		return
	}
	a.render(w, "login", page{Title: "Sign in"})
}

func (a *app) login(w http.ResponseWriter, r *http.Request) {
	user := strings.ToLower(strings.TrimSpace(r.PostFormValue("user")))
	if !checkLogin(user, r.PostFormValue("password")) {
		time.Sleep(time.Second)
		w.WriteHeader(http.StatusUnauthorized)
		a.render(w, "login", page{Title: "Sign in", Err: "Incorrect username or password.", D: user})
		return
	}
	a.sess.set(w, r, user)
	http.Redirect(w, r, "/", http.StatusSeeOther)
}

func (a *app) logout(w http.ResponseWriter, r *http.Request) {
	a.sess.clear(w)
	http.Redirect(w, r, "/login", http.StatusSeeOther)
}

// ------------------------------------------------------------ main page

type row struct {
	Entry
	Kind  string // "rec" or "tx"
	ID    int
	Class string
}

type month struct {
	Label string
	Rows  []row
}

type mainData struct {
	Projection
	Back   int
	Months []month
}

func (a *app) mainPage(w http.ResponseWriter, r *http.Request, user string) {
	b, err := a.store.Load(user)
	if err != nil {
		a.fail(w, err)
		return
	}
	today := a.today()
	back, _ := strconv.Atoi(r.URL.Query().Get("back"))
	back = max(0, min(back, 3660))
	p := b.Project(today, today-Date(back), DateOf(today.Time().AddDate(1, 0, 0)))

	d := mainData{Projection: p, Back: back}
	for i, e := range p.Entries {
		rw := row{Entry: e, Kind: "tx", ID: e.TxID}
		if e.RecID != 0 {
			rw.Kind, rw.ID = "rec", e.RecID
		}
		var cls []string
		if e.Date < today {
			cls = append(cls, "past")
		}
		if e.Skipped {
			cls = append(cls, "skip")
		}
		if i == p.LowestIdx {
			cls = append(cls, "low")
		}
		if e.Balance < 0 {
			cls = append(cls, "od")
		}
		rw.Class = strings.Join(cls, " ")

		label := e.Date.Time().Format("January 2006")
		if len(d.Months) == 0 || d.Months[len(d.Months)-1].Label != label {
			d.Months = append(d.Months, month{Label: label})
		}
		m := &d.Months[len(d.Months)-1]
		m.Rows = append(m.Rows, rw)
	}
	a.render(w, "main", page{Title: "Projection", Page: "main", User: user,
		Msg: r.URL.Query().Get("msg"), D: d})
}

// editEntry handles the edit dialog for both one-offs (kind=tx) and occurrences (kind=rec).
func (a *app) editEntry(w http.ResponseWriter, r *http.Request, user string) {
	f := r.PostFormValue
	target := "/"
	if back, _ := strconv.Atoi(f("back")); back > 0 {
		target = fmt.Sprintf("/?back=%d", back)
	}
	action := f("action")
	id, _ := strconv.Atoi(f("id"))

	var date Date
	var amount Money
	desc := strings.TrimSpace(f("desc"))
	if action == "save" {
		var err1, err2 error
		date, err1 = ParseDate(f("date"))
		amount, err2 = ParseMoney(f("amount"))
		if err1 != nil || err2 != nil || desc == "" {
			redirect(w, r, target, "Not saved: date, description and amount are all required.")
			return
		}
	}

	var msg string
	err := a.store.Update(user, func(b *Budget) error {
		if f("kind") == "rec" {
			orig, err := ParseDate(f("orig"))
			if err != nil {
				return err
			}
			switch action {
			case "save":
				return b.EditOccurrence(id, orig, date, amount, desc)
			case "skip":
				return b.SkipOccurrence(id, orig)
			case "restore":
				b.RestoreOccurrence(id, orig)
				return nil
			}
		} else {
			switch action {
			case "save":
				if id == 0 {
					b.AddTransaction(date, desc, amount, false)
					msg = "Added " + desc + "."
					return nil
				}
				return b.UpdateTransaction(id, date, desc, amount)
			case "delete":
				return b.RemoveTransaction(id)
			}
		}
		return fmt.Errorf("unknown action %q", action)
	})
	if err != nil {
		msg = "Not saved: " + err.Error()
	}
	redirect(w, r, target, msg)
}

func (a *app) reconcile(w http.ResponseWriter, r *http.Request, user string) {
	bank, err := ParseMoney(r.PostFormValue("balance"))
	if err != nil {
		redirect(w, r, "/", "Not reconciled: "+err.Error())
		return
	}
	var diff Money
	if err := a.store.Update(user, func(b *Budget) error {
		diff = b.Reconcile(a.today(), bank)
		return nil
	}); err != nil {
		a.fail(w, err)
		return
	}
	msg := "Already reconciled: balance matches."
	if diff != 0 {
		msg = fmt.Sprintf("Reconciled: added adjustment of %s today.", diff)
	}
	redirect(w, r, "/", msg)
}

// ------------------------------------------------------- recurring page

// recRow holds form values as strings so invalid input can be shown back as typed.
type recRow struct {
	ID, Desc, Amount, Freq, Interval, Start, End, Prio string
}

type recurringData struct {
	Rows  []recRow
	Blank recRow
}

func (a *app) recurringPage(w http.ResponseWriter, r *http.Request, user string) {
	b, err := a.store.Load(user)
	if err != nil {
		a.fail(w, err)
		return
	}
	var rows []recRow
	for _, rc := range b.Recurring {
		rw := recRow{
			ID: strconv.Itoa(rc.ID), Desc: rc.Desc, Amount: rc.Amount.Plain(), Freq: rc.Freq,
			Interval: strconv.Itoa(rc.Interval), Start: rc.Start.String(),
		}
		if rc.End != nil {
			rw.End = rc.End.String()
		}
		if rc.Priority != nil {
			rw.Prio = strconv.Itoa(*rc.Priority)
		}
		rows = append(rows, rw)
	}
	a.renderRecurring(w, user, rows, r.URL.Query().Get("msg"), "")
}

func (a *app) renderRecurring(w http.ResponseWriter, user string, rows []recRow, msg, errMsg string) {
	blank := recRow{Freq: Monthly, Interval: "1", Start: a.today().String()}
	a.render(w, "recurring", page{Title: "Recurring", Page: "recurring", User: user,
		Msg: msg, Err: errMsg, D: recurringData{Rows: rows, Blank: blank}})
}

func (a *app) saveRecurring(w http.ResponseWriter, r *http.Request, user string) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	field := func(k string, i int) string {
		if v := r.PostForm[k]; i < len(v) {
			return strings.TrimSpace(v[i])
		}
		return ""
	}

	var rows []recRow
	var list []Recurring
	var errs []string
	for i := range r.PostForm["id"] {
		rw := recRow{
			ID: field("id", i), Desc: field("desc", i), Amount: field("amount", i), Freq: field("freq", i),
			Interval: field("interval", i), Start: field("start", i), End: field("end", i), Prio: field("prio", i),
		}
		rows = append(rows, rw)
		rc, err := parseRecRow(rw)
		if err != nil {
			errs = append(errs, fmt.Sprintf("Row %d: %v", i+1, err))
			continue
		}
		list = append(list, rc)
	}

	if len(errs) == 0 {
		err := a.store.Update(user, func(b *Budget) error { return b.SetRecurring(list) })
		if err == nil {
			redirect(w, r, "/recurring", "Saved.")
			return
		}
		errs = append(errs, err.Error())
	}
	w.WriteHeader(http.StatusUnprocessableEntity)
	a.renderRecurring(w, user, rows, "", strings.Join(errs, "\n"))
}

func parseRecRow(rw recRow) (Recurring, error) {
	var rc Recurring
	var err error
	rc.ID, _ = strconv.Atoi(rw.ID)
	rc.Desc, rc.Freq = rw.Desc, rw.Freq
	if rc.Amount, err = ParseMoney(rw.Amount); err != nil {
		return rc, err
	}
	if rc.Interval, err = strconv.Atoi(rw.Interval); err != nil {
		return rc, fmt.Errorf("invalid interval %q", rw.Interval)
	}
	if rc.Start, err = ParseDate(rw.Start); err != nil {
		return rc, err
	}
	if rw.End != "" {
		end, err := ParseDate(rw.End)
		if err != nil {
			return rc, err
		}
		rc.End = &end
	}
	if rw.Prio != "" {
		p, err := strconv.Atoi(rw.Prio)
		if err != nil {
			return rc, fmt.Errorf("invalid priority %q", rw.Prio)
		}
		rc.Priority = &p
	}
	return rc, rc.Validate()
}
