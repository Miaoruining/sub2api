//go:build embed

package web

import (
	"encoding/xml"
	"strings"
	"testing"
	"time"
)

func TestEditorialDatesAndDiscovery(t *testing.T) {
	body, err := renderPublicSitemap()
	if err != nil {
		t.Fatal(err)
	}
	var sitemap sitemapURLSet
	if err := xml.Unmarshal(body, &sitemap); err != nil {
		t.Fatal(err)
	}
	if sitemap.URLs[0].LastMod != "" {
		t.Fatal("dynamic homepage must not have a fabricated modification date")
	}
	for _, page := range publicSEOPages {
		date := publicSEOUpdated(page.Path)
		if _, err := time.Parse("2006-01-02", date); err != nil {
			t.Fatal(err)
		}
		html, err := renderPublicSEOPage(page, "test")
		if err != nil {
			t.Fatal(err)
		}
		for _, want := range []string{`datetime="` + date + `"`, `"dateModified":"` + date + `"`} {
			if !strings.Contains(string(html), want) {
				t.Errorf("%s missing %s", page.Path, want)
			}
		}
		found := false
		for _, entry := range sitemap.URLs {
			if entry.Loc == canonicalSEOURL(page.Path) {
				found = entry.LastMod == date
			}
		}
		if !found {
			t.Errorf("sitemap date missing for %s", page.Path)
		}
		footer := strings.SplitN(string(html), "<footer>", 2)
		if len(footer) != 2 {
			t.Fatal("missing footer")
		}
		for _, guide := range publicSEOPages {
			if !strings.Contains(footer[1], `href="`+guide.Path+`"`) {
				t.Errorf("%s does not link to %s", page.Path, guide.Path)
			}
		}
	}
}
