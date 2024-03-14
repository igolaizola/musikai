package distrokid

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/PuerkitoBio/goquery"
	"github.com/chromedp/cdproto/network"
	"github.com/chromedp/cdproto/page"
	"github.com/chromedp/chromedp"
	"github.com/go-rod/stealth"
	"github.com/igolaizola/musikai/pkg/ratelimit"
	"github.com/igolaizola/musikai/pkg/session"
)

type Browser struct {
	ctx             context.Context
	cancel          context.CancelFunc
	cancelAllocator context.CancelFunc
	rateLimit       ratelimit.Lock
	remote          string
	proxy           string
	profile         bool
	cookieStore     CookieStore
}

type BrowserConfig struct {
	Wait        time.Duration
	Remote      string
	Proxy       string
	Profile     bool
	CookieStore CookieStore
}

func NewBrowser(cfg *BrowserConfig) *Browser {
	wait := cfg.Wait
	if wait == 0 {
		wait = 1 * time.Second
	}
	return &Browser{
		remote:      cfg.Remote,
		proxy:       cfg.Proxy,
		profile:     cfg.Profile,
		cookieStore: cfg.CookieStore,
		rateLimit:   ratelimit.New(wait),
	}
}

func (b *Browser) Start(ctx context.Context) error {
	// Obtain the cookie
	rawCookies, err := b.cookieStore.GetCookie(ctx)
	if err != nil {
		return err
	}
	if rawCookies == "" {
		return fmt.Errorf("distrokid: cookie is empty")
	}
	cookies, err := session.UnmarshalCookies(rawCookies, nil)
	if err != nil {
		return fmt.Errorf("distrokid: couldn't parse cookie: %w", err)
	}

	// Create a new context
	if b.remote != "" {
		log.Println("distrokid: connecting to browser at", b.remote)
		ctx, b.cancelAllocator = chromedp.NewRemoteAllocator(ctx, b.remote)
	} else {
		log.Println("distrokid: launching browser")
		opts := append(
			chromedp.DefaultExecAllocatorOptions[3:],
			chromedp.NoFirstRun,
			chromedp.NoDefaultBrowserCheck,
			chromedp.Flag("headless", false),
		)

		if b.proxy != "" {
			opts = append(opts,
				chromedp.ProxyServer(b.proxy),
			)
		}

		if b.profile {
			opts = append(opts,
				// if user-data-dir is set, chrome won't load the default profile,
				// even if it's set to the directory where the default profile is stored.
				// set it to empty to prevent chromedp from setting it to a temp directory.
				chromedp.UserDataDir(""),
				chromedp.Flag("disable-extensions", false),
			)
		}
		ctx, b.cancelAllocator = chromedp.NewExecAllocator(ctx, opts...)
	}

	// create chrome instance
	ctx, b.cancel = chromedp.NewContext(
		ctx,
		// chromedp.WithDebugf(log.Printf),
	)
	b.ctx = ctx

	// Launch stealth plugin
	if err := chromedp.Run(
		ctx,
		chromedp.Evaluate(stealth.JS, nil),
	); err != nil {
		return fmt.Errorf("distrokid: could not launch stealth plugin: %w", err)
	}

	// disable webdriver
	if err := chromedp.Run(ctx, chromedp.ActionFunc(func(cxt context.Context) error {
		_, err := page.AddScriptToEvaluateOnNewDocument("Object.defineProperty(navigator, 'webdriver', { get: () => false, });").Do(cxt)
		if err != nil {
			return err
		}
		return nil
	})); err != nil {
		return fmt.Errorf("could not disable webdriver: %w", err)
	}

	// Actions to set the cookie and navigate
	if err := chromedp.Run(ctx,
		chromedp.ActionFunc(func(ctx context.Context) error {
			for _, cookie := range cookies {
				err := network.SetCookie(cookie.Name, cookie.Value).
					WithDomain("distrokid.com").
					//WithHTTPOnly(true).
					Do(ctx)
				if err != nil {
					return fmt.Errorf("distrokid: could not set cookie: %w", err)
				}
			}
			return nil
		}),
	); err != nil {
		return fmt.Errorf("distrokid: could not set cookie and navigate: %w", err)
	}

	if err := chromedp.Run(ctx,
		// Load google first to have a sane referer
		chromedp.Navigate("https://www.google.com/"),
		chromedp.WaitReady("body", chromedp.ByQuery),
		chromedp.Navigate("https://distrokid.com/profile/"),
		chromedp.WaitReady("body", chromedp.ByQuery),
	); err != nil {
		return fmt.Errorf("distrokid: could not obtain chatgpt data: %w", err)
	}

	// Obtain the document
	var html string
	if err := chromedp.Run(ctx,
		chromedp.OuterHTML("html", &html),
	); err != nil {
		return fmt.Errorf("distrokid: couldn't get html: %w", err)
	}

	// Get user ID
	userID, err := getUserID(html)
	if err != nil {
		return fmt.Errorf("distrokid: couldn't get user ID: %w", err)
	}
	fmt.Println("user id:", userID)

	return nil
}

// Stop closes the browser.
func (c *Browser) Stop() error {
	// Obtain cookies after navigation
	var cs []*network.Cookie
	if err := chromedp.Run(c.ctx,
		chromedp.ActionFunc(func(ctx context.Context) error {
			candidate, err := network.GetCookies().WithUrls([]string{"https://distrokid.com"}).Do(c.ctx)
			if err != nil {
				return fmt.Errorf("distrokid: could not get cookies: %w", err)
			}
			cs = candidate
			return nil
		}),
	); err != nil {
		return fmt.Errorf("distrokid: could not get cookies: %w", err)
	}

	// Set the cookie
	var cookies []*http.Cookie
	for _, cookie := range cs {
		cookies = append(cookies, &http.Cookie{
			Name:  cookie.Name,
			Value: cookie.Value,
		})
	}
	raw := session.MarshalCookies(cookies)
	if err := c.cookieStore.SetCookie(c.ctx, raw); err != nil {
		return fmt.Errorf("distrokid: couldn't set cookie: %w", err)
	}

	chromedp.Cancel(c.ctx)
	c.cancel()
	c.cancelAllocator()
	return nil
}

type Album struct {
	Artist    string
	FirstName string
	LastName  string
	Title     string
	Songs     []Song
}

type Song struct {
	Title string
	Path  string
}

// Publish publishes a new album
func (c *Browser) Publish(parent context.Context, album *Album) error {
	// Create a new tab based on client context
	ctx, cancel := chromedp.NewContext(c.ctx)
	defer cancel()

	ctx, cancel = context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	// Navigate to the new album page
	if err := chromedp.Run(ctx,
		chromedp.Navigate("https://distrokid.com/new/"),
		chromedp.WaitVisible("body", chromedp.ByQuery),
	); err != nil {
		return fmt.Errorf("distrokid: couldn't navigate to url: %w", err)
	}

	// Obtain the document
	var html string
	if err := chromedp.Run(ctx,
		chromedp.OuterHTML("html", &html),
	); err != nil {
		return fmt.Errorf("distrokid: couldn't get html: %w", err)
	}

	// Get user ID
	userID, err := getUserID(html)
	if err != nil {
		return fmt.Errorf("distrokid: couldn't get user ID: %w", err)
	}
	fmt.Println("user id:", userID)

	// Load the document into goquery
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(html))
	if err != nil {
		return fmt.Errorf("distrokid: couldn't parse html: %w", err)
	}

	// Get album UUID
	albumUUID, err := getAlbumUUID(doc)
	if err != nil {
		return fmt.Errorf("distrokid: couldn't get albumuuid: %w", err)
	}
	log.Println("album uuid:", albumUUID)

	time.Sleep(5 * time.Second)

	key := "1 song (a single)"
	if len(album.Songs) > 1 {
		key = fmt.Sprintf("%d songs", len(album.Songs))
	}
	log.Println("choose number of songs", key)
	if err := chromedp.Run(ctx,
		chromedp.SendKeys(`#howManySongsOnThisAlbum`, key, chromedp.ByQuery),
	); err != nil {
		return fmt.Errorf("distrokid: couldn't click on select box: %w", err)
	}

	log.Println("set artist name")
	if err := chromedp.Run(ctx,
		chromedp.SetValue(`#artistName`, album.Artist),
	); err != nil {
		return fmt.Errorf("distrokid: couldn't set artist name: %w", err)
	}

	log.Println("set album title")
	if err := chromedp.Run(ctx,
		chromedp.SetValue(`#albumTitle`, album.Title),
	); err != nil {
		return fmt.Errorf("distrokid: couldn't set album title: %w", err)
	}

	// Obtain the document
	if err := chromedp.Run(ctx,
		chromedp.OuterHTML("html", &html),
	); err != nil {
		return fmt.Errorf("distrokid: couldn't get html: %w", err)
	}
	doc, err = goquery.NewDocumentFromReader(strings.NewReader(html))
	if err != nil {
		return fmt.Errorf("distrokid: couldn't parse html: %w", err)
	}
	// Obtain
	// <input type="hidden" name="tracknum_10db4c2e-880f-cab4-15df-209800d330f5" id="tracknum_10db4c2e-880f-cab4-15df-209800d330f5" value="1">
	trackIDs := make([]string, len(album.Songs))
	doc.Find("input[name^=tracknum_]").Each(func(i int, s *goquery.Selection) {
		v, ok := s.Attr("value")
		if !ok {
			log.Println("couldn't find tracknum")
			return
		}
		num, err := strconv.Atoi(v)
		if err != nil {
			log.Println("couldn't parse tracknum")
			return
		}
		id, ok := s.Attr("id")
		if !ok {
			log.Println("couldn't find id")
			return
		}
		id = strings.TrimPrefix(id, "tracknum_")
		trackIDs[num-1] = id
	})
	for i, id := range trackIDs {
		if id == "" {
			return fmt.Errorf("distrokid: couldn't find track id for song %d", i+1)
		}
	}

	for i, song := range album.Songs {
		n := i + 1
		id := trackIDs[i]
		log.Printf("set song title %d", n)
		if err := chromedp.Run(ctx,
			chromedp.SetValue(fmt.Sprintf("title_%s", id), song.Title),
		); err != nil {
			return fmt.Errorf("distrokid: couldn't set song title: %w", err)
		}
	}
	<-time.After(35 * time.Second)

	return nil
}

func getAlbumUUID(doc *goquery.Document) (string, error) {
	albumUUID, exists := doc.Find("#albumuuid").Attr("value")
	if !exists {
		return "", fmt.Errorf("distrokid: couldn't find albumuuid")
	}
	if albumUUID == "" {
		return "", fmt.Errorf("distrokid: albumuuid is empty")
	}
	return albumUUID, nil
}

func getUserID(html string) (int, error) {
	// Find the first match in the HTML content
	varPattern := regexp.MustCompile(`(?s)var me\s*=\s*({.*?});`)
	matches := varPattern.FindStringSubmatch(html)
	if len(matches) < 2 {
		return 0, fmt.Errorf("distrokid: couldn't find me object")
	}
	// Extract the javascript object
	js := matches[1]

	// Replace single quotes with double quotes
	js = strings.ReplaceAll(js, "'", "\"")
	// Add quotes around keys
	js = strings.ReplaceAll(js, "://", "$//")
	re := regexp.MustCompile(`(\w+)\s*:`)
	js = re.ReplaceAllString(js, `"$1":`)
	js = strings.ReplaceAll(js, "$//", "://")
	// Remove last comma
	re = regexp.MustCompile(`\s*(,)\s*}`)
	js = re.ReplaceAllString(js, "\n}")

	fmt.Println(js)

	var me meResponse
	if err := json.Unmarshal([]byte(js), &me); err != nil {
		return 0, fmt.Errorf("distrokid: couldn't unmarshal me response: %w", err)
	}
	if me.ID == 0 {
		return 0, fmt.Errorf("distrokid: couldn't find user ID")
	}
	return me.ID, nil
}
