// Command budget serves a small personal-budget web app: recurring transactions,
// a one-year running-balance projection and bank reconciliation.
package main

import (
	"crypto/sha256"
	"embed"
	"encoding/hex"
	"flag"
	"html/template"
	"io/fs"
	"log"
	"net/http"
	"os"
	"time"
	_ "time/tzdata"
)

//go:embed web
var webFS embed.FS

func main() {
	addr := flag.String("addr", ":9312", "listen address")
	dataDir := flag.String("data", "data", "directory for budget data")
	tz := flag.String("tz", "", "time zone for \"today\", e.g. Australia/Sydney (default: system)")
	cert := flag.String("cert", "", "TLS certificate file (optional)")
	key := flag.String("key", "", "TLS key file (optional)")
	flag.Parse()

	loc := time.Local
	if *tz != "" {
		var err error
		if loc, err = time.LoadLocation(*tz); err != nil {
			log.Fatal(err)
		}
	}
	if err := os.MkdirAll(*dataDir, 0o700); err != nil {
		log.Fatal(err)
	}
	store := &Store{dir: *dataDir}
	if err := store.Migrate(); err != nil {
		log.Fatal(err)
	}

	a := &app{
		store: store,
		loc:   loc,
		pages: map[string]*template.Template{},
	}
	a.loadAssets()

	log.Printf("budget: listening on %s", *addr)
	h := sameOrigin(a.routes())
	if *cert != "" && *key != "" {
		log.Fatal(http.ListenAndServeTLS(*addr, *cert, *key, h))
	}
	log.Fatal(http.ListenAndServe(*addr, h))
}

func (a *app) loadAssets() {
	funcs := template.FuncMap{
		"plain": Money.Plain,
		"day":   func(d Date) string { return d.Time().Format("Mon 02") },
		"long":  func(d Date) string { return d.Time().Format("2 Jan 2006") },
	}
	for _, p := range []string{"main", "recurring"} {
		a.pages[p] = template.Must(template.New("").Funcs(funcs).ParseFS(webFS,
			"web/templates/layout.html", "web/templates/"+p+".html"))
	}

	// Version static URLs by content so the service worker cache never serves stale files.
	h := sha256.New()
	fs.WalkDir(webFS, "web/static", func(p string, d fs.DirEntry, err error) error {
		if err == nil && !d.IsDir() {
			b, _ := webFS.ReadFile(p)
			h.Write(b)
		}
		return nil
	})
	a.version = hex.EncodeToString(h.Sum(nil))[:10]
	a.icons = makeIcons()
}

// sameOrigin rejects cross-site form posts (there is no login, so this is the only CSRF guard).
func sameOrigin(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost {
			if o := r.Header.Get("Origin"); o != "" && o != "http://"+r.Host && o != "https://"+r.Host {
				http.Error(w, "cross-origin request refused", http.StatusForbidden)
				return
			}
		}
		next.ServeHTTP(w, r)
	})
}
