package dns

import (
	"strings"
	"testing"

	"github.com/miekg/dns"
)

// loadLines builds a filter with one set parsed from the given lines.
func loadLines(t *testing.T, name string, lines ...string) *Filter {
	t.Helper()
	f := NewFilter()
	n, err := f.LoadSet(name, strings.NewReader(strings.Join(lines, "\n")))
	if err != nil {
		t.Fatalf("load set: %v", err)
	}
	if n == 0 && len(lines) > 0 {
		t.Fatalf("no rules parsed from %d lines", len(lines))
	}
	return f
}

func TestFilterParseFormats(t *testing.T) {
	f := loadLines(t, SetAdblock,
		"# comment",
		"! adblock comment",
		"[Adblock Plus 2.0]",
		"0.0.0.0 ads.example.org",
		"127.0.0.1\ttracker.example.org",
		"255.255.255.255 broadcasthost", // loopback-ish name: skipped
		"::1 localhost",                 // skipped
		"plain-domain.example.net",
		"||banner.example.com^",
		"||popup.example.com",
		"0.0.0.0 spaced.example.org # trailing comment",
		"", // empty
	)

	blocked := []string{
		"ads.example.org",
		"www.ads.example.org", // suffix walk
		"tracker.example.org",
		"plain-domain.example.net",
		"banner.example.com",
		"img.banner.example.com", // suffix of ||rule
		"popup.example.com",
		"spaced.example.org",
	}
	for _, d := range blocked {
		if ok, set := f.Match([]string{SetAdblock}, d); !ok || set != SetAdblock {
			t.Errorf("expected %s blocked, got ok=%v set=%q", d, ok, set)
		}
	}

	allowed := []string{
		"example.org",
		"notads.example.org",  // no substring false-positives
		"good.banner.example", // suffix walk must not cross the rule domain
		"broadcasthost",
		"localhost",
	}
	for _, d := range allowed {
		if ok, _ := f.Match([]string{SetAdblock}, d); ok {
			t.Errorf("expected %s allowed", d)
		}
	}

	// Trailing dots and case are normalized.
	if ok, _ := f.Match([]string{SetAdblock}, "ADS.Example.ORG."); !ok {
		t.Error("expected case/dot-insensitive match")
	}
}

func TestFilterGarbage(t *testing.T) {
	f := loadLines(t, SetAdblock,
		"##.banner",               // cosmetic CSS
		"example.com##div.ad",     // domain + CSS
		"example.com#$#hide-it",   // CSS with delimiter
		"/^ad.*\\.example$/",      // regex rule
		"http://example.com/path", // URL
		"this is not a domain line at all !",
		"-invalid-.domain",
		"||bad domain.example^", // space inside pattern
		"1.2.3.4.5.6.7.8",       // digits+dots: syntactically a domain, must parse
		"||good.example^$image", // unknown option: still a block
	)
	if ok, _ := f.Match([]string{SetAdblock}, "good.example"); !ok {
		t.Error("rule with unknown $option must still block its domain")
	}
	// "1.2.3.4.5.6.7.8" is a syntactically valid domain name and is accepted;
	// real garbage above must not have crashed the parser or blocked anything.
	if ok, _ := f.Match([]string{SetAdblock}, "example.com"); ok {
		t.Error("cosmetic rule must not block the domain itself")
	}
}

func TestFilterExceptions(t *testing.T) {
	f := loadLines(t, SetAdblock,
		"||example.org^",
		"||ads.example.org^",
		"@@||ads.example.org^",
		"@@plain-allowed.example.net",
	)
	// Whitelist wins over the block of the same (or another) set.
	if ok, _ := f.Match([]string{SetAdblock}, "ads.example.org"); ok {
		t.Error("@@ exception must override the block")
	}
	if ok, _ := f.Match([]string{SetAdblock}, "sub.ads.example.org"); ok {
		t.Error("@@ exception must cover subdomains")
	}
	if ok, _ := f.Match([]string{SetAdblock}, "example.org"); !ok {
		t.Error("non-excluded domain must stay blocked")
	}
	// Exception in one set protects against blocks in another set.
	f2 := loadLines(t, SetCustom, "@@cross.example.org")
	f2.LoadSet(SetAdblock, strings.NewReader("||cross.example.org^"))
	if ok, _ := f2.Match([]string{SetAdblock, SetCustom}, "cross.example.org"); ok {
		t.Error("exception must win across sets")
	}
}

func TestFilterDNSRewrite(t *testing.T) {
	f := loadLines(t, SetSafesearch,
		"||google.com^$dnsrewrite=NOERROR;CNAME;forcesafesearch.google.com",
		"||yandex.ru^$dnsrewrite=NOERROR;A;213.180.193.56",
		"||broken.example^$dnsrewrite=NOERROR;TXT;hello", // unsupported type: skipped
	)
	rr, set, ok := f.Rewrite([]string{SetSafesearch}, "www.google.com")
	if !ok || set != SetSafesearch {
		t.Fatalf("expected safesearch rewrite for www.google.com, ok=%v", ok)
	}
	if rr.rrType != dns.TypeCNAME || rr.value != "forcesafesearch.google.com." {
		t.Errorf("unexpected rewrite: %+v", rr)
	}
	rr, _, ok = f.Rewrite([]string{SetSafesearch}, "yandex.ru")
	if !ok || rr.value != "213.180.193.56" {
		t.Errorf("expected A rewrite for yandex.ru, got %+v ok=%v", rr, ok)
	}
	if _, _, ok := f.Rewrite([]string{SetSafesearch}, "broken.example"); ok {
		t.Error("unsupported rewrite type must be skipped")
	}
	if ok, _ := f.Match([]string{SetSafesearch}, "broken.example"); ok {
		t.Error("a skipped rewrite must not silently become a block")
	}
}

func TestFilterBuiltinSets(t *testing.T) {
	f := NewFilter()
	lens := f.SetsLen()
	if lens[SetDoH] != len(dohDomains) {
		t.Errorf("doh set: got %d rules, want %d", lens[SetDoH], len(dohDomains))
	}
	if ok, set := f.Match([]string{SetDoH}, "cloudflare-dns.com"); !ok || set != SetDoH {
		t.Error("cloudflare-dns.com must be blocked by the doh set")
	}
	if ok, _ := f.Match([]string{SetDoH}, "example.com"); ok {
		t.Error("doh set must not block unrelated domains")
	}

	if _, set, ok := f.Rewrite([]string{SetSafesearch}, "www.youtube.com"); !ok || set != SetSafesearch {
		t.Fatal("youtube must hit the safesearch set")
	}
	rr, _, ok := f.Rewrite([]string{SetSafesearch}, "yandex.ru")
	if !ok || rr.rrType != dns.TypeA || rr.value != "213.180.193.56" {
		t.Errorf("yandex A rewrite: %+v ok=%v", rr, ok)
	}
}

func TestFilterAtomicReplace(t *testing.T) {
	f := loadLines(t, SetAdblock, "||old.example^")
	if _, err := f.LoadSet(SetAdblock, strings.NewReader("||new.example^")); err != nil {
		t.Fatalf("reload: %v", err)
	}
	if ok, _ := f.Match([]string{SetAdblock}, "old.example"); ok {
		t.Error("replaced rule must be gone")
	}
	if ok, _ := f.Match([]string{SetAdblock}, "new.example"); !ok {
		t.Error("new rule must be active")
	}
	f.DropSet(SetAdblock)
	if ok, _ := f.Match([]string{SetAdblock}, "new.example"); ok {
		t.Error("dropped set must not match")
	}
}
