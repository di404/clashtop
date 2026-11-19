// mockclash is a fake mihomo external-controller for developing ClashTop
// without touching a real Clash instance.
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"math/rand"
	"net/http"
	"strings"
	"sync"
	"time"
)

type conn struct {
	ID       string            `json:"id"`
	Metadata map[string]string `json:"metadata"`
	Upload   int64             `json:"upload"`
	Download int64             `json:"download"`
	Start    time.Time         `json:"start"`
	Chains   []string          `json:"chains"`
	Rule     string            `json:"rule"`
	Payload  string            `json:"rulePayload"`
	rateUp   int64
	rateDown int64
	ttl      time.Time
}

var (
	hosts = []string{"www.google.com", "api.github.com", "github.githubassets.com", "i.ytimg.com",
		"rr3---sn-a5mekn6s.googlevideo.com", "api.anthropic.com", "imap.gmail.com", "cdn.jsdelivr.net",
		"www.bilibili.com", "upos-sz-mirrorcos.bilivideo.com", "registry.npmjs.org", "proxy.golang.org",
		"telegram.org", "149.154.167.51", "update.code.visualstudio.com", "www.apple.com"}
	procs  = []string{"Google Chrome", "Safari", "Telegram", "Code Helper", "node", "go", "Notes", "", "curl"}
	nodes  = []string{"🇸🇬 Relay-SG1", "🇯🇵 Tokyo-02", "🇺🇸 LA-03", "🇭🇰 HK-01", "DIRECT"}
	groups = []string{"PROXY", "AI", "Streaming", "Final"}
	rules  = [][2]string{{"DomainSuffix", "google.com"}, {"DomainKeyword", "github"}, {"GeoSite", "youtube"},
		{"Match", ""}, {"GeoIP", "CN"}, {"RuleSet", "telegram"}, {"ProcessName", "curl"}}
	levels = []string{"debug", "debug", "debug", "info", "info", "warning", "error"}
	tags   = []string{"[DNS]", "[TCP]", "[UDP]", "[Rule]", "[TUN]", "[Sniffer]"}
)

type state struct {
	mu        sync.Mutex
	conns     map[string]*conn
	upTotal   int64
	downTotal int64
	lastUp    int64
	lastDown  int64
	seq       int
}

func (s *state) spawn(now time.Time) {
	s.seq++
	r := rules[rand.Intn(len(rules))]
	node := nodes[rand.Intn(len(nodes))]
	chains := []string{node, groups[rand.Intn(len(groups))]}
	if node == "DIRECT" {
		chains = []string{"DIRECT"}
	}
	host := hosts[rand.Intn(len(hosts))]
	proc := procs[rand.Intn(len(procs))]
	heavy := rand.Intn(6) == 0
	c := &conn{
		ID: fmt.Sprintf("%08x-mock-%04d", rand.Uint32(), s.seq),
		Metadata: map[string]string{
			"network": "tcp", "type": "HTTPS", "sourceIP": "127.0.0.1",
			"sourcePort": fmt.Sprint(50000 + rand.Intn(15000)), "destinationIP": "",
			"destinationPort": "443", "host": host, "process": proc,
			"processPath": "/Applications/" + proc + ".app", "inboundName": "DEFAULT-MIXED",
			"remoteDestination": fmt.Sprintf("104.18.%d.%d", rand.Intn(255), rand.Intn(255)), "dnsMode": "normal",
		},
		Start:  now,
		Chains: chains, Rule: r[0], Payload: r[1],
		rateUp: int64(rand.Intn(2000)), rateDown: int64(rand.Intn(20000)),
		ttl: now.Add(time.Duration(3+rand.Intn(60)) * time.Second),
	}
	if heavy {
		c.rateDown = int64(200_000 + rand.Intn(3_000_000))
		c.rateUp = int64(rand.Intn(80_000))
		c.ttl = now.Add(time.Duration(20+rand.Intn(120)) * time.Second)
	}
	if proc == "" {
		c.Metadata["processPath"] = "/usr/libexec/mDNSResponder"
	}
	if rand.Intn(12) == 0 { // mihomo reports absurd counters for STUN
		c.Metadata["network"], c.Metadata["type"] = "udp", "Tun"
		c.Metadata["host"], c.Metadata["destinationPort"] = "stun.l.google.com", "19302"
		c.rateDown, c.rateUp = 3_000_000_000, 2_000_000_000
	} else if rand.Intn(5) == 0 {
		c.Metadata["network"] = "udp"
		c.Metadata["type"] = "Tun"
		c.Metadata["destinationPort"] = "443"
	}
	s.conns[c.ID] = c
}

func (s *state) step() {
	s.mu.Lock()
	defer s.mu.Unlock()
	now := time.Now()
	for i := rand.Intn(5); i > 0; i-- {
		if len(s.conns) < 80 {
			s.spawn(now)
		}
	}
	var up, down int64
	for id, c := range s.conns {
		if now.After(c.ttl) {
			delete(s.conns, id)
			continue
		}
		jitter := func(v int64) int64 {
			if v == 0 {
				return 0
			}
			return v/2 + rand.Int63n(v)
		}
		du, dd := jitter(c.rateUp), jitter(c.rateDown)
		if rand.Intn(3) == 0 { // idle tick
			du, dd = 0, 0
		}
		c.Upload += du
		c.Download += dd
		up += du
		down += dd
	}
	s.upTotal += up
	s.downTotal += down
	s.lastUp, s.lastDown = up, down
}

func main() {
	addr := flag.String("addr", "127.0.0.1:19090", "listen address")
	secret := flag.String("secret", "", "require this bearer secret")
	flag.Parse()

	s := &state{conns: map[string]*conn{}, upTotal: 1 << 30, downTotal: 12 << 30}
	for i := 0; i < 20; i++ {
		s.spawn(time.Now().Add(-time.Duration(rand.Intn(600)) * time.Second))
	}
	go func() {
		for range time.Tick(time.Second) {
			s.step()
		}
	}()

	auth := func(h http.HandlerFunc) http.HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) {
			if *secret != "" && r.Header.Get("Authorization") != "Bearer "+*secret {
				http.Error(w, `{"message":"Unauthorized"}`, http.StatusUnauthorized)
				return
			}
			h(w, r)
		}
	}
	streamEvery := func(w http.ResponseWriter, r *http.Request, d time.Duration, fn func() any) {
		fl := w.(http.Flusher)
		enc := json.NewEncoder(w)
		t := time.NewTicker(d)
		defer t.Stop()
		for {
			if err := enc.Encode(fn()); err != nil {
				return
			}
			fl.Flush()
			select {
			case <-r.Context().Done():
				return
			case <-t.C:
			}
		}
	}

	http.HandleFunc("/version", auth(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `{"meta":true,"version":"mock-1.19"}`)
	}))
	http.HandleFunc("/traffic", auth(func(w http.ResponseWriter, r *http.Request) {
		streamEvery(w, r, time.Second, func() any {
			s.mu.Lock()
			defer s.mu.Unlock()
			return map[string]int64{"up": s.lastUp, "down": s.lastDown, "upTotal": s.upTotal, "downTotal": s.downTotal}
		})
	}))
	http.HandleFunc("/memory", auth(func(w http.ResponseWriter, r *http.Request) {
		first := true
		streamEvery(w, r, time.Second, func() any {
			if first {
				first = false
				return map[string]int64{"inuse": 0, "oslimit": 0}
			}
			return map[string]int64{"inuse": 60<<20 + rand.Int63n(20<<20), "oslimit": 0}
		})
	}))
	http.HandleFunc("/logs", auth(func(w http.ResponseWriter, r *http.Request) {
		min := map[string]int{"debug": 0, "info": 3, "warning": 5, "error": 6}[r.URL.Query().Get("level")]
		fl := w.(http.Flusher)
		enc := json.NewEncoder(w)
		for {
			i := min + rand.Intn(len(levels)-min)
			msg := tags[rand.Intn(len(tags))] + " " + hosts[rand.Intn(len(hosts))]
			switch levels[i] {
			case "debug":
				msg += " --> [104.18.32.7 104.18.33.7] A, cache hit, expire at " + time.Now().Add(time.Minute).Format(time.DateTime)
			case "info":
				msg = fmt.Sprintf("[TCP] 127.0.0.1:%d --> %s:443 match %s using %s", 50000+rand.Intn(9999),
					hosts[rand.Intn(len(hosts))], rules[rand.Intn(len(rules))][0], nodes[rand.Intn(len(nodes))])
			case "warning":
				msg += " dial timeout, retrying"
			case "error":
				msg = "[TCP] dial " + nodes[rand.Intn(4)] + " error: connect failed: i/o timeout"
			}
			if enc.Encode(map[string]string{"type": levels[i], "payload": msg}) != nil {
				return
			}
			fl.Flush()
			select {
			case <-r.Context().Done():
				return
			case <-time.After(time.Duration(50+rand.Intn(400)) * time.Millisecond):
			}
		}
	}))
	http.HandleFunc("/connections", auth(func(w http.ResponseWriter, r *http.Request) {
		s.mu.Lock()
		defer s.mu.Unlock()
		if r.Method == http.MethodDelete {
			s.conns = map[string]*conn{}
			w.WriteHeader(http.StatusNoContent)
			return
		}
		list := make([]*conn, 0, len(s.conns))
		for _, c := range s.conns {
			list = append(list, c)
		}
		json.NewEncoder(w).Encode(map[string]any{
			"downloadTotal": s.downTotal, "uploadTotal": s.upTotal, "connections": list, "memory": 0,
		})
	}))
	http.HandleFunc("/connections/", auth(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodDelete {
			http.NotFound(w, r)
			return
		}
		s.mu.Lock()
		delete(s.conns, strings.TrimPrefix(r.URL.Path, "/connections/"))
		s.mu.Unlock()
		w.WriteHeader(http.StatusNoContent)
	}))
	log.Printf("mockclash listening on %s", *addr)
	log.Fatal(http.ListenAndServe(*addr, nil))
}
