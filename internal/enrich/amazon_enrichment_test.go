package enrich

import (
	"bytes"
	"context"
	"errors"
	"image"
	"image/png"
	"net/url"
	"strings"
	"testing"

	"secretsanta/internal/ssrf"
)

type fakeFetcher struct {
	page    string
	status  int
	pageErr error
	imgErr  error
	img     *ssrf.Image
	pages   []string
}

func (f *fakeFetcher) FetchPage(_ context.Context, raw string) (*ssrf.Response, error) {
	f.pages = append(f.pages, raw)
	if f.pageErr != nil {
		return nil, f.pageErr
	}
	u, _ := url.Parse(raw)
	st := f.status
	if st == 0 {
		st = 200
	}
	return &ssrf.Response{URL: u, StatusCode: st, ContentType: "text/html", Body: []byte(f.page)}, nil
}

func (f *fakeFetcher) FetchImage(_ context.Context, _ string) (*ssrf.Image, error) {
	return f.img, f.imgErr
}

func pngImage(t *testing.T) *ssrf.Image {
	t.Helper()
	var b bytes.Buffer
	if err := png.Encode(&b, image.NewRGBA(image.Rect(0, 0, 4, 4))); err != nil {
		t.Fatal(err)
	}
	img, err := ssrf.CheckImage(b.Bytes())
	if err != nil {
		t.Fatal(err)
	}
	return img
}

func newEnricher(t *testing.T, f *fakeFetcher) *Amazon {
	t.Helper()
	st, err := ssrf.NewStore(t.TempDir(), "/images")
	if err != nil {
		t.Fatal(err)
	}
	return &Amazon{Fetcher: f, Images: st}
}

const amazonURL = "https://www.amazon.fr/dp/B000TEST"

func TestEnrichExtractsTitleImagePriceFromPage(t *testing.T) {
	f := &fakeFetcher{img: pngImage(t), page: `<html><head><title>Amazon.fr : truc</title></head><body>
<span id="productTitle">  Théière   en fonte </span>
<img id="landingImage" src="/images/I/abc.jpg">
<span class="a-offscreen">24,90 €</span></body></html>`}
	got := newEnricher(t, f).Enrich(context.Background(), Suggestion{URL: amazonURL})
	if got.Title != "Théière en fonte" {
		t.Errorf("titre = %q", got.Title)
	}
	if got.PriceCents != 2490 || got.Currency != "EUR" {
		t.Errorf("prix = %d %q", got.PriceCents, got.Currency)
	}
	if !strings.HasPrefix(got.ImageSrc, "/images/") {
		t.Errorf("image = %q, attendu une copie locale", got.ImageSrc)
	}
}

func TestEnrichUsesJSONLD(t *testing.T) {
	f := &fakeFetcher{img: pngImage(t), page: `<html><head><script type="application/ld+json">
{"@type":"Product","name":"Lampe","image":["https://m.media-amazon.com/x.png"],"offers":{"@type":"Offer","price":"1299.00","priceCurrency":"EUR"}}
</script></head></html>`}
	got := newEnricher(t, f).Enrich(context.Background(), Suggestion{URL: amazonURL})
	if got.Title != "Lampe" || got.PriceCents != 129900 || got.Currency != "EUR" || got.ImageSrc == "" {
		t.Errorf("résultat = %+v", got)
	}
}

func TestEnrichKeepsSuggestionOnFailure(t *testing.T) {
	orig := Suggestion{URL: amazonURL, Title: "Ma théière", PriceCents: 2000, Currency: "EUR"}
	cases := map[string]*fakeFetcher{
		"réseau":      {pageErr: errors.New("boom")},
		"statut":      {status: 503, page: "x"},
		"page vide":   {page: ""},
		"html bancal": {page: "<<<div <span id=productTitle"},
	}
	for name, f := range cases {
		t.Run(name, func(t *testing.T) {
			got := newEnricher(t, f).Enrich(context.Background(), orig)
			if got != orig {
				t.Errorf("suggestion modifiée : %+v", got)
			}
		})
	}
}

func TestEnrichImageFailureKeepsTitleAndPrice(t *testing.T) {
	f := &fakeFetcher{imgErr: ssrf.ErrNotImage, page: `<meta property="og:title" content="Livre"><meta property="og:image" content="https://x/y.png"><meta property="product:price:amount" content="9.99"><meta property="product:price:currency" content="EUR">`}
	got := newEnricher(t, f).Enrich(context.Background(), Suggestion{URL: amazonURL})
	if got.Title != "Livre" || got.PriceCents != 999 || got.ImageSrc != "" {
		t.Errorf("résultat = %+v", got)
	}
}

func TestEnrichDoesNotOverwriteKnownFields(t *testing.T) {
	f := &fakeFetcher{page: `<span id="productTitle">Autre</span><span class="a-offscreen">5,00 €</span>`}
	got := newEnricher(t, f).Enrich(context.Background(), Suggestion{URL: amazonURL, Title: "Mien", PriceCents: 100})
	if got.Title != "Mien" || got.PriceCents != 100 {
		t.Errorf("résultat = %+v", got)
	}
}

func TestEnrichIgnoresNonAmazon(t *testing.T) {
	f := &fakeFetcher{page: `<span id="productTitle">X</span>`}
	s := Suggestion{URL: "https://example.com/dp/1"}
	if got := newEnricher(t, f).Enrich(context.Background(), s); got != s || len(f.pages) != 0 {
		t.Errorf("une page non Amazon a été récupérée : %+v %v", got, f.pages)
	}
}

func TestIsAmazonURL(t *testing.T) {
	yes := []string{"https://www.amazon.fr/dp/1", "https://amazon.com/x", "https://smile.amazon.co.uk/x", "https://amzn.to/abc", "https://a.co/d/x"}
	no := []string{"https://example.com", "https://amazon.evil.example.com/x", "https://notamazon.fr", "ftp://amazon.fr/x", "https://example.com/amazon.fr", "::"}
	for _, u := range yes {
		if !IsAmazonURL(u) {
			t.Errorf("%s devrait être Amazon", u)
		}
	}
	for _, u := range no {
		if IsAmazonURL(u) {
			t.Errorf("%s ne devrait pas être Amazon", u)
		}
	}
}

func TestParsePrice(t *testing.T) {
	for in, want := range map[string]int{
		"12,99 €": 1299, "$1,299.00": 129900, "19.9": 1990, "1 299,50 €": 129950, "20": 2000, "1.299": 129900,
	} {
		if got, ok := ParsePrice(in); !ok || got != want {
			t.Errorf("ParsePrice(%q) = %d,%v ; attendu %d", in, got, ok, want)
		}
	}
	if _, ok := ParsePrice("gratuit"); ok {
		t.Error("« gratuit » n'est pas un prix")
	}
}
