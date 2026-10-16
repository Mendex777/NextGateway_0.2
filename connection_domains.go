package main

import (
	"net"
	"regexp"
	"strings"
	"time"
)

// The access log has no session ID. Only attach a sniffed name when the
// process-scoped session and the full available tuple identify one event.
// Never infer a domain from a shared CDN address or a DNS/PTR lookup.
const connectionJoinWindow = 3 * time.Second

var connectionInfo = regexp.MustCompile(`\[Info\] \[([0-9]+)\] (proxy/dokodemo|app/dispatcher): (.+)$`)
var connectionRoute = regexp.MustCompile(`taking detour \[([^\]]+)\] for \[(tcp|udp):([^\]]+)\]`)
var connectionRule = regexp.MustCompile(`^Hit route rule: \[([^\]]+)\] so taking detour `)
var connectionDomain = regexp.MustCompile(`^[A-Za-z0-9_][A-Za-z0-9_.-]{0,252}$`)

type domainSession struct {
	process, source, domain, network, port, outbound, rule string
	at                                                     time.Time
}
type domainResolver struct{ sessions map[string]*domainSession }

func (r *domainResolver) observe(message, process string, at time.Time) {
	m := connectionInfo.FindStringSubmatch(message)
	if m == nil || process == "" {
		return
	}
	if r.sessions == nil {
		r.sessions = map[string]*domainSession{}
	}
	key := process + "|" + m[1]
	s := r.sessions[key]
	if s == nil {
		s = &domainSession{process: process}
		r.sessions[key] = s
	}
	s.at = at
	body := m[3]
	if m[2] == "proxy/dokodemo" && strings.HasPrefix(body, "received request for ") {
		endpoint := strings.TrimPrefix(body, "received request for ")
		host, port, e := net.SplitHostPort(endpoint)
		if e != nil || net.ParseIP(host) == nil {
			return
		}
		// A reused session ID cannot inherit a previous connection's domain.
		*s = domainSession{process: process, source: net.JoinHostPort(host, port), at: at}
	} else if m[2] == "app/dispatcher" {
		if strings.HasPrefix(body, "sniffed domain: ") {
			domain := strings.TrimSuffix(strings.TrimSpace(strings.TrimPrefix(body, "sniffed domain: ")), ".")
			if connectionDomain.MatchString(domain) && net.ParseIP(domain) == nil {
				s.domain = strings.ToLower(domain)
			}
		}
		if route := connectionRoute.FindStringSubmatch(body); route != nil {
			host, port, e := net.SplitHostPort(route[3])
			if e == nil {
				s.network = route[2]
				s.port = port
				s.outbound = route[1]
				if match := connectionRule.FindStringSubmatch(body); match != nil {
					s.rule = connectionRuleLabel(match[1])
				}
				if s.domain != "" && !strings.EqualFold(strings.TrimSuffix(host, "."), s.domain) {
					s.domain = ""
				}
			}
		} else if strings.HasPrefix(body, "default route for ") {
			s.rule = "Маршрут по умолчанию"
			target := strings.TrimPrefix(body, "default route for ")
			for _, network := range []string{"tcp", "udp"} {
				if strings.HasPrefix(target, network+":") {
					_, port, e := net.SplitHostPort(strings.TrimPrefix(target, network+":"))
					if e == nil {
						s.network = network
						s.port = port
					}
				}
			}
		}
	}
	// Keep correlation state short lived and bounded even under a connection burst.
	if len(r.sessions) > 8192 {
		r.prune(at)
		if len(r.sessions) > 8192 {
			r.sessions = map[string]*domainSession{}
		}
	}
}
func (r *domainResolver) prune(now time.Time) {
	for id, s := range r.sessions {
		if now.Sub(s.at) > connectionJoinWindow {
			delete(r.sessions, id)
		}
	}
}
func (r *domainResolver) enrich(events []ConnectionEvent, now time.Time) {
	type candidate struct {
		s       *domainSession
		matches []int
	}
	bySource := map[string][]*candidate{}
	for _, s := range r.sessions {
		if s.source != "" && s.network != "" && s.port != "" {
			key := s.process + "|" + s.source
			bySource[key] = append(bySource[key], &candidate{s: s})
		}
	}
	possibilities := map[int][]*candidate{}
	for i := len(events) - 1; i >= 0; i-- {
		event := &events[i]
		if now.Sub(event.Time) > connectionJoinWindow {
			break
		}
		key := event.process + "|" + net.JoinHostPort(event.Source, event.SourcePort)
		for _, candidate := range bySource[key] {
			s := candidate.s
			delta := event.Time.Sub(s.at)
			if delta < 0 {
				delta = -delta
			}
			if delta <= connectionJoinWindow && s.network == event.Network && s.port == event.Port && (s.outbound == "" || s.outbound == event.Outbound) {
				candidate.matches = append(candidate.matches, i)
				possibilities[i] = append(possibilities[i], candidate)
			}
		}
	}
	for i, possible := range possibilities {
		events[i].Domain = ""
		events[i].Rule = ""
		if len(possible) == 1 && len(possible[0].matches) == 1 {
			events[i].Domain = possible[0].s.domain
			events[i].Rule = possible[0].s.rule
		}
	}
	r.prune(now)
}
