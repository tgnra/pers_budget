# Budget

A small personal-budget web app: recurring transactions, a one-year running-balance
projection, per-occurrence edits and bank reconciliation. It's a single Go binary with no
third-party dependencies. Each account's data is kept in `data/<username>.json`.

## Build

Requires Go 1.24+.

1. `cp accounts.example.txt accounts.txt` and edit it. Each line is `username:password`.
   The file is git-ignored.
2. Run `./build.ps1` (Windows) or `./build.sh`.
   - `go generate` hashes the passwords (PBKDF2-SHA256, 600k iterations) into
     `accounts_gen.go`, which is also git-ignored. Only the hashes are compiled in.
   - After any change to `accounts.txt`, rebuild.

To cross-compile for a server, run e.g. `GOOS=linux GOARCH=arm64 ./build.sh`.

## Run

```
budget -addr :9312 -data ./data -tz Australia/Sydney
```

| flag | default | |
|---|---|---|
| `-addr` | `:9312` | listen address |
| `-data` | `data` | account JSON files and `.session_key` |
| `-tz` | system | time zone that decides "today" |
| `-cert`, `-key` | | serve TLS directly |

Installing as a PWA and the service worker both need HTTPS, except on `localhost`. Either
pass `-cert/-key` or put the app behind a reverse proxy (Caddy, nginx, Tailscale serve)
that sets `X-Forwarded-Proto: https`.

## Use

- **Recurring**: one row per series. Use **+ Add** to add a row and **−** to remove one, then **Save**.
  - Frequency is *Monthly* (keeps the start day, clamped in shorter months), *Month end*
    (the last day of every month) or *Weekly*.
  - *Every* N means every Nth week or month.
  - *Priority* orders transactions that fall on the same day (1 first). Rows without a
    priority go after the numbered ones.
- **Projection** lists everything from today to a year out, with a running balance.
  - The top of the page shows today's balance and the lowest point in the next 12 months.
  - Tap a row to edit it. For a recurring occurrence you can change its date, amount or
    description, skip it, or restore it to the series default. One-off transactions can be
    edited or deleted. **+ Transaction** adds a one-off.
- **Reconcile**: enter your actual bank balance. An adjustment dated today is added to settle
  the difference, which is also how you set your opening balance.

The balance is the sum of every transaction up to a date, past ones included. That means
editing a series changes its past occurrences too. Reconcile after changing a series.
