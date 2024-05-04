package main

import (
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"

	"github.com/bluesky-social/indigo/atproto/syntax"
	"github.com/hashicorp/golang-lru/v2/expirable"
	"github.com/miekg/dns"
)

type HTTPResolver struct {
	client      *http.Client
	backendHost string
	suffix      string
	ttl         int
	cache       *expirable.LRU[syntax.Handle, syntax.DID]
}

// represents JSON response from backend com.atproto.identity.resolveHandle request
type ResolveResp struct {
	DID string `json:"did"`
}

func (hr *HTTPResolver) Run(bind string) error {
	srv := &dns.Server{Addr: bind, Net: "udp"}
	dns.HandleFunc(".", hr.handleTXT)
	slog.Info("listening on UDP", "bind", bind, "backendHost", hr.backendHost, "ttl", hr.ttl, "suffix", hr.suffix)
	return srv.ListenAndServe()
}

func (hr *HTTPResolver) parseDomain(domain string) (syntax.Handle, error) {
	domain = strings.ToLower(domain)
	if !strings.HasPrefix(domain, "_atproto.") {
		return "", fmt.Errorf("missing _atproto prefix")
	}
	domain = strings.TrimPrefix(domain, "_atproto.")
	domain = strings.TrimSuffix(domain, ".")
	if hr.suffix != "" && !strings.HasSuffix(domain, hr.suffix) {
		return "", fmt.Errorf("does not have required suffix: %s", hr.suffix)
	}
	return syntax.ParseHandle(domain)
}

func (hr *HTTPResolver) resolveHandle(hdl syntax.Handle) (syntax.DID, error) {

	// first try cache
	if hr.cache != nil {
		val, ok := hr.cache.Get(hdl)
		if ok {
			return val, nil
		}
	}

	req, err := http.NewRequest("GET", fmt.Sprintf("%s/xrpc/com.atproto.identity.resolveHandle?handle=%s", hr.backendHost, hdl), nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("User-Agent", "indigo-handlr")
	resp, err := hr.client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode == 404 || resp.StatusCode == 400 {
		return "", nil
	}
	if resp.StatusCode != 200 {
		return "", fmt.Errorf("failed to resolve handle status=%d", resp.StatusCode)
	}
	bodyBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", err
	}
	var resolveResp ResolveResp
	if err = json.Unmarshal(bodyBytes, &resolveResp); err != nil {
		return "", err
	}
	did, err := syntax.ParseDID(resolveResp.DID)
	if err != nil {
		return "", err
	}
	if hr.cache != nil {
		hr.cache.Add(hdl, did)
	}
	return did, nil
}

func (hr *HTTPResolver) handleTXT(w dns.ResponseWriter, r *dns.Msg) {
	//fmt.Printf("%s", r)
	msg := dns.Msg{}
	msg.SetReply(r)
	if len(r.Question) == 0 {
		w.WriteMsg(&msg)
		return
	}
	// TODO: what about multiple questions?
	switch r.Question[0].Qtype {
	case dns.TypeTXT:
		msg.Authoritative = true // TODO: configurable?
		domain := msg.Question[0].Name
		hdl, err := hr.parseDomain(domain)
		slog.Debug("DNS TXT request", "domain", domain, "handle", hdl, "parseErr", err)
		if err != nil {
			w.WriteMsg(&msg)
			return
		}
		did, err := hr.resolveHandle(hdl)
		if err != nil {
			slog.Error("error resolving handle", "handle", hdl, "err", err)
			msg.SetRcode(r, dns.RcodeServerFailure)
			w.WriteMsg(&msg)
			return
		}
		if did == "" {
			// not found: NXDOMAIN
			msg.SetRcode(r, dns.RcodeNameError)
			w.WriteMsg(&msg)
			return
		}
		msg.Answer = append(msg.Answer, &dns.TXT{
			Hdr: dns.RR_Header{
				Name:   domain,
				Rrtype: dns.TypeTXT,
				Class:  dns.ClassINET,
				Ttl:    uint32(hr.ttl),
			},
			Txt: []string{"did=" + did.String()},
		})
	}
	w.WriteMsg(&msg)
}
